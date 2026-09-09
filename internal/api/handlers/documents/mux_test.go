package documents

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestMetadataBearerAuthAndVersioning(t *testing.T) {
	// Cases share a key and organization that are deleted after the read checks.
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "api-docs", "API Docs")
	otherID := testutil.CreateTestOrg(t, db, "other-docs", "Other Docs")
	token := func(org int, name string, scope enums.Scope) string {
		_, value, err := apikeys.Create(t.Context(), db, org, userID, &apikeys.CreateOptions{Name: name, Scopes: []enums.Scope{scope}})
		require.NoError(t, err)
		return value
	}
	readToken := token(orgID, "read", enums.ScopeRead)
	writeToken := token(orgID, "write", enums.ScopeWrite)
	otherToken := token(otherID, "other", enums.ScopeRead)
	doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "report.txt", ContentType: "text/plain", Size: 4})
	require.NoError(t, err)
	doc, err = documents.MarkUploaded(t.Context(), db, orgID, doc.ExternalID)
	require.NoError(t, err)
	router := mux.New(mux.Config{Middleware: []mux.Middleware{authentication.RequireAPIKey(db), version.Middleware}})
	Mount(router, db, nil, nil)
	for _, tt := range []struct {
		name, token, path, version, method string
		status                             int
		code                               string
	}{
		{name: "list", token: readToken, path: "/documents", status: http.StatusOK},
		{name: "get version", token: readToken, path: "/documents/" + doc.ExternalID, version: "2026-01-01", status: http.StatusOK},
		{name: "missing bearer", path: "/documents", status: http.StatusUnauthorized, code: "APIKEY-09"},
		{name: "invalid bearer", token: "invalid", path: "/documents", status: http.StatusUnauthorized, code: "APIKEY-09"},
		{name: "insufficient scope", token: writeToken, path: "/documents", status: http.StatusForbidden, code: "APIKEY-10"},
		{name: "unsupported version", token: readToken, path: "/documents", version: "2099-01-01", status: http.StatusBadRequest, code: "API-01"},
		{name: "other tenant", token: otherToken, path: "/documents/" + doc.ExternalID, status: http.StatusNotFound, code: "HTTP-404"},
		{name: "invalid UUID", token: readToken, path: "/documents/not-a-uuid", status: http.StatusNotFound},
		{name: "content missing bearer", path: "/documents/" + doc.ExternalID + "/content", status: http.StatusUnauthorized, code: "APIKEY-09"},
		{name: "content insufficient scope", token: writeToken, path: "/documents/" + doc.ExternalID + "/content", status: http.StatusForbidden, code: "APIKEY-10"},
		{name: "content unsupported version", token: readToken, path: "/documents/" + doc.ExternalID + "/content", version: "2099-01-01", status: http.StatusBadRequest, code: "API-01"},
		{name: "content invalid UUID", token: readToken, path: "/documents/not-a-uuid/content", status: http.StatusNotFound},
		{name: "read key cannot upload", token: readToken, path: "/documents", method: http.MethodPost, status: http.StatusMethodNotAllowed},
		{name: "write key cannot upload", token: writeToken, path: "/documents", method: http.MethodPost, status: http.StatusMethodNotAllowed},
		{name: "unsupported method", token: readToken, path: "/documents", method: http.MethodDelete, status: http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			req.Header.Set("X-API-Version", tt.version)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			if method == http.MethodPost {
				require.NotContains(t, rec.Header().Get("Allow"), http.MethodPost)
				require.Contains(t, rec.Header().Get("Allow"), http.MethodGet)
			}
			if tt.code != "" {
				require.Contains(t, rec.Body.String(), tt.code)
			}
			if tt.status == http.StatusOK {
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				require.Contains(t, rec.Body.String(), doc.ExternalID)
				require.NotContains(t, rec.Body.String(), doc.ObjectKey)
				require.NotContains(t, rec.Body.String(), "organization_id")
				require.Contains(t, rec.Body.String(), `"status":"uploaded"`)
			}
		})
	}
	require.NoError(t, organizations.Delete(t.Context(), db, orgID))
	req := httptest.NewRequest(http.MethodGet, "/documents", nil)
	req.Header.Set("Authorization", "Bearer "+readToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
