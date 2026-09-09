package documents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestMetadataReads(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx, org := actor(t, db)
	router := mux.New(mux.Config{})
	NewMux(db, nil, nil).Mount(router, "/documents")
	first, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "report.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: 1}}})
	require.NoError(t, err)
	hash := strings.Repeat("a", 64)
	first, err = documents.PublishPDF(t.Context(), db, org.ID, first.ExternalID, first.ObjectKey+"/pdf/"+hash, 123)
	require.NoError(t, err)
	second := createDocument(t, db, org.ID, true)
	uploading := createDocument(t, db, org.ID, false)
	failed := createDocument(t, db, org.ID, false)
	require.NoError(t, documents.MarkFailed(t.Context(), db, org.ID, failed.ExternalID))
	failed.Status = enums.DocumentStatusFailed
	otherID := testutil.CreateTestOrg(t, db, "other", "Other")
	other := createDocument(t, db, otherID, true)

	var page pagination.Page[*documents.Document]
	path := "/documents?limit=1"
	for i, doc := range []*documents.Document{failed, uploading, second, first} {
		rec := request(router, ctx, http.MethodGet, path)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.Equal(t, doc.ExternalID, page.Data[0].ExternalID)
		require.Equal(t, doc.Status, page.Data[0].Status)
		require.Equal(t, doc.SHA256, page.Data[0].SHA256)
		require.Equal(t, i < 3, page.HasMore)
		if page.HasMore {
			require.NotEmpty(t, page.NextCursor)
			path = "/documents?limit=1&cursor=" + url.QueryEscape(page.NextCursor)
		}
	}

	rec := request(router, ctx, http.MethodGet, "/documents/"+first.ExternalID)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	var response struct {
		Document map[string]any `json:"document"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Document, 9)
	require.Equal(t, enums.DocumentStatusUploaded.String(), response.Document["status"])
	require.Equal(t, first.ExternalID, response.Document["id"])
	require.Equal(t, "report.pdf", response.Document["filename"])
	require.Equal(t, "application/pdf", response.Document["content_type"])
	require.Equal(t, float64(123), response.Document["size"])
	require.Equal(t, float64(1), response.Document["page_count"])
	require.Equal(t, hash, response.Document["sha256"])
	require.NotEmpty(t, response.Document["created_at"])
	require.NotEmpty(t, response.Document["updated_at"])
	require.NotContains(t, rec.Body.String(), first.ObjectKey)
	for _, doc := range []*documents.Document{uploading, failed} {
		rec = request(router, ctx, http.MethodGet, "/documents/"+doc.ExternalID)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Len(t, response.Document, 9)
		require.Equal(t, doc.Status.String(), response.Document["status"])
		require.Equal(t, "", response.Document["sha256"])
		require.NotContains(t, rec.Body.String(), "organization_id")
		require.NotContains(t, rec.Body.String(), doc.ObjectKey)
	}
	rec = request(router, ctx, http.MethodGet, "/documents/"+other.ExternalID)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "HTTP-404")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
	rec = request(router, ctx, http.MethodGet, "/documents/"+first.ExternalID)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = request(router, ctx, http.MethodGet, "/documents")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Empty(t, page.Data)
}

func TestMetadataAccessGuard(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"no context", "missing organization", "missing session", "missing user", "missing member", "mismatched user", "mismatched organization", "synthetic admin", "invalid role", "unpersisted API key", "API key other organization", "write-only API key", "viewer", "read API key"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx, org := actor(t, db)
			member := *organizations.MemberFromContext(ctx)
			want := http.StatusForbidden
			code := "DOC-02"
			switch name {
			case "no context":
				ctx = t.Context()
				code = "DOC-01"
			case "missing organization":
				ctx = organizations.WithContext(ctx, nil)
				code = "DOC-01"
			case "missing session":
				ctx = sessions.WithContext(ctx, 0)
			case "missing user":
				ctx = users.WithContext(ctx, nil)
			case "missing member":
				ctx = organizations.WithMemberContext(ctx, nil)
			case "mismatched user":
				member.UserID++
				ctx = organizations.WithMemberContext(ctx, &member)
			case "mismatched organization":
				member.OrganizationID++
				ctx = organizations.WithMemberContext(ctx, &member)
			case "synthetic admin":
				member.ID = 0
				member.Role = organizations.RoleOwner
				ctx = organizations.WithMemberContext(ctx, &member)
			case "invalid role":
				member.Role = "invalid"
				ctx = organizations.WithMemberContext(ctx, &member)
			case "unpersisted API key":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
			case "API key other organization":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID + 1, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
			case "write-only API key":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeWrite}})
			case "viewer":
				want = http.StatusOK
			case "read API key":
				ctx = organizations.WithContext(t.Context(), org)
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
				want = http.StatusOK
			}
			router := mux.New(mux.Config{})
			NewMux(db, nil, nil).Mount(router, "/documents")
			rec := request(router, ctx, http.MethodGet, "/documents")
			require.Equal(t, want, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if want != http.StatusOK {
				require.Contains(t, rec.Body.String(), code)
			}
		})
	}
}

func TestMetadataPaginationAndRouting(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx, org := actor(t, db)
	router := mux.New(mux.Config{})
	NewMux(db, nil, nil).Mount(router, "/documents")
	for _, cursor := range []string{"%%%", "bnVsbA", pagination.Encode(pagination.Cursor{"id": "1"}), pagination.Encode(pagination.Cursor{"id": "1", "organization_id": strconv.Itoa(org.ID + 1)})} {
		rec := request(router, ctx, http.MethodGet, "/documents?cursor="+url.QueryEscape(cursor))
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), `"code":"DOC-03"`)
		require.Contains(t, rec.Body.String(), `"field":"cursor"`)
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
	// Router fallbacks intentionally disclose only route shape and invoke no metadata handler.
	rec := request(router, t.Context(), http.MethodGet, "/documents/not-a-uuid")
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = request(router, t.Context(), http.MethodDelete, "/documents")
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Contains(t, rec.Header().Get("Allow"), http.MethodGet)
}

func actor(t *testing.T, db database.Database) (context.Context, *organizations.Organization) {
	t.Helper()
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "documents", "Documents")
	org, err := organizations.Get(t.Context(), db, orgID)
	require.NoError(t, err)
	member, err := organizations.CreateMember(t.Context(), db, userID, orgID, organizations.RoleViewer, optional.Empty[string]())
	require.NoError(t, err)
	ctx := organizations.WithContext(t.Context(), org)
	ctx = organizations.WithMemberContext(ctx, member)
	ctx = users.WithContext(ctx, &users.User{ID: userID})
	return sessions.WithContext(ctx, 1), org
}

func createDocument(t *testing.T, db database.Database, orgID int, uploaded bool) *documents.Document {
	t.Helper()
	doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "report.pdf", ContentType: "application/pdf", Size: 123})
	require.NoError(t, err)
	if uploaded {
		doc, err = documents.MarkUploaded(t.Context(), db, orgID, doc.ExternalID)
		require.NoError(t, err)
	}
	return doc
}

func request(router http.Handler, ctx context.Context, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, path, nil).WithContext(ctx))
	return rec
}
