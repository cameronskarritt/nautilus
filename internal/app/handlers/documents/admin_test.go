package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/mocks"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

const adminDocuments = "/admin/organizations/{orgID:<uuid>}/documents"

func TestAdminIntakeRejectsBeforeIO(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"anonymous", "non-admin", "no session", "unpersisted user", "API key", "API key with admin session", "assumed organization without admin"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := users.WithContext(t.Context(), &users.User{ID: 1, Admin: true})
			ctx = sessions.WithContext(ctx, 1)
			switch name {
			case "anonymous":
				ctx = t.Context()
			case "non-admin", "assumed organization without admin":
				ctx = users.WithContext(ctx, &users.User{ID: 1})
				ctx = sessions.WithAssumedOrgID(ctx, optional.Set(2))
			case "no session":
				ctx = sessions.WithContext(ctx, 0)
			case "unpersisted user":
				ctx = users.WithContext(ctx, &users.User{Admin: true})
			case "API key":
				ctx = apikeys.WithContext(t.Context(), &apikeys.Key{ID: 1, OrganizationID: 1, Scopes: []apikeys.Scope{apikeys.ScopeWrite}})
			case "API key with admin session":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1})
			}
			keys := new(uploadKeys)
			router := mux.New(mux.Config{})
			NewMux(nil, nil, nil).MountAdmin(router, adminDocuments, keys)
			for _, suffix := range []string{"", "/11111111-1111-4111-8111-111111111111", "/11111111-1111-4111-8111-111111111111/content", "/11111111-1111-4111-8111-111111111111/text"} {
				method := http.MethodGet
				if suffix == "" {
					method = http.MethodPost
				}
				body := new(unreadUploadBody)
				req := httptest.NewRequest(method, "/admin/organizations/11111111-1111-4111-8111-111111111111/documents"+suffix, body).WithContext(ctx)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusForbidden, rec.Code)
				require.Contains(t, rec.Body.String(), `"code":"ADMIN-01"`)
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				require.Zero(t, body.reads)
			}
			require.Zero(t, keys.orgCalls)
			require.Zero(t, keys.userCalls)
		})
	}
}

func TestAdminIntake(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "intake", "Intake")
	org, err := organizations.Get(t.Context(), db, orgID)
	require.NoError(t, err)
	otherID := testutil.CreateTestOrg(t, db, "other", "Other")
	other, err := organizations.Get(t.Context(), db, otherID)
	require.NoError(t, err)
	ctx := users.WithContext(t.Context(), &users.User{ID: userID, Admin: true})
	ctx = sessions.WithContext(ctx, 1)
	ctx = sessions.WithAssumedOrgID(ctx, optional.Set(other.ID))
	ctx = organizations.WithContext(ctx, other)
	keys := new(uploadKeys)
	store := new(contentStore)
	workflows := mocks.NewClient(t)
	workflows.On("ExecuteWorkflow", mock.Anything, mock.Anything, upload.Name, mock.Anything).Run(func(args mock.Arguments) {
		require.Equal(t, org.ID, args.Get(3).(upload.Input).OrganizationID)
	}).Return(nil, nil).Once()
	router := mux.New(mux.Config{})
	m := NewMux(db, store, workflows)
	m.MountAdmin(router, adminDocuments, keys)
	m.Mount(router, "/documents")
	path := "/admin/organizations/" + org.ExternalID + "/documents"
	data := uploadImage(t, "png")
	req := multipartRequest(t, func(w *multipart.Writer) {
		part, err := w.CreateFormFile("file", "private-name.png")
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
	}).WithContext(ctx)
	req.URL.Path = path
	req.Header.Set("X-Organization-Slug", other.Slug)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var response struct {
		Document documents.Document `json:"document"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	doc, err := documents.GetByExternalID(ctx, db, org.ID, response.Document.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, doc)
	plaintext, err := encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, store.data, encrypt.Binding{Purpose: "document-page", RecordID: doc.ExternalID + "/1"})
	require.NoError(t, err)
	require.Equal(t, data, plaintext)
	rec = request(router, ctx, http.MethodGet, path+"/"+doc.ExternalID)
	require.Equal(t, http.StatusOK, rec.Code)
	for _, suffix := range []string{"", "/content", "/text"} {
		rec = request(router, ctx, http.MethodGet, "/admin/organizations/"+other.ExternalID+"/documents/"+doc.ExternalID+suffix)
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
	require.Zero(t, store.gets)
	pdf := []byte("synthetic PDF")
	hash := sha256.Sum256(pdf)
	doc, err = documents.PublishPDF(ctx, db, org.ID, doc.ExternalID, doc.ObjectKey+"/pdf/"+hex.EncodeToString(hash[:]), int64(len(pdf)))
	require.NoError(t, err)
	store.data, err = encrypt.ForOrganization(keys, org.ExternalID).Seal(ctx, pdf, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
	require.NoError(t, err)
	rec = request(router, ctx, http.MethodGet, path+"/"+doc.ExternalID+"/content")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, string(pdf), rec.Body.String())
	store.read = 0
	store.data, err = encrypt.ForOrganization(keys, org.ExternalID).Seal(ctx, []byte("synthetic OCR text"), encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	require.NoError(t, err)
	rec = request(router, ctx, http.MethodGet, path+"/"+doc.ExternalID+"/text")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "synthetic OCR text", rec.Body.String())
	for _, kind := range []enums.AuditType{enums.AuditTypeDocumentUpload, enums.AuditTypeDocumentContent, enums.AuditTypeDocumentText} {
		var payload []byte
		require.NoError(t, db.QueryRow(ctx, "SELECT payload FROM audit_logs WHERE actor_id = $1 AND target_org_id = $2 AND type = $3", userID, org.ID, kind).Scan(&payload))
		require.JSONEq(t, `{"document_id":"`+doc.ExternalID+`"}`, string(payload))
	}
	require.Zero(t, keys.userCalls)
	// Privileged intake never changes the ordinary membership boundary.
	rec = request(router, organizations.WithContext(ctx, org), http.MethodGet, "/documents/"+doc.ExternalID)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NoError(t, organizations.Delete(ctx, db, org.ID))
	calls := keys.orgCalls
	body := new(unreadUploadBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, body).WithContext(ctx))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Zero(t, body.reads)
	require.Equal(t, calls, keys.orgCalls)
}

func TestOrdinaryDocumentRoutesCannotUpload(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		admin bool
	}{
		{name: "member"},
		{name: "admin", admin: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			org := &organizations.Organization{ID: 1, ExternalID: "11111111-1111-4111-8111-111111111111"}
			ctx := users.WithContext(t.Context(), &users.User{ID: 1, Admin: tt.admin})
			ctx = sessions.WithContext(ctx, 1)
			ctx = organizations.WithContext(ctx, org)
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: enums.RoleOwner})
			body := new(unreadUploadBody)
			router := mux.New(mux.Config{})
			NewMux(nil, nil, nil).Mount(router, "/documents")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents", body).WithContext(ctx))
			require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			require.Contains(t, rec.Header().Get("Allow"), http.MethodGet)
			require.NotContains(t, rec.Header().Get("Allow"), http.MethodPost)
			require.Zero(t, body.reads)
		})
	}
}
