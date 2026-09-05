package apikeys

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCurrentAPIKeyUsesBearerAuthVersioningAndReadScope(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "current-api-key"})
	organizationID := testutil.CreateTestOrg(t, db, "current-api-key", "Current API Key")
	readKey, readToken, err := apikeys.Create(
		t.Context(),
		db,
		organizationID,
		userID,
		&apikeys.CreateOptions{Name: "Read", Scopes: []apikeys.Scope{apikeys.ScopeRead}},
	)
	require.NoError(t, err)
	_, writeToken, err := apikeys.Create(
		t.Context(),
		db,
		organizationID,
		userID,
		&apikeys.CreateOptions{Name: "Write", Scopes: []apikeys.Scope{apikeys.ScopeWrite}},
	)
	require.NoError(t, err)

	router := mux.New(mux.Config{Middleware: []mux.Middleware{
		authentication.RequireAPIKey(db),
		version.Middleware,
	}})
	Mount(router)

	rec := currentAPIKeyRequest(router, readToken)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.NotContains(t, rec.Body.String(), readToken)
	var response struct {
		APIKey *apikeys.Key `json:"api_key"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotNil(t, response.APIKey)
	require.Equal(t, readKey.ExternalID, response.APIKey.ExternalID)

	rec = currentAPIKeyRequest(router, writeToken)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "APIKEY-10")
}

func currentAPIKeyRequest(router http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api-keys/current", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
