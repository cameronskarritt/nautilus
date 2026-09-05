package apikeys

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestAPIKeyLifecycle(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	router, actor := newTestRouter(t, db, organizations.RoleOwner, "lifecycle")

	rec := apiKeyRequest(
		t,
		router,
		actor,
		http.MethodPost,
		"/api-keys",
		`{"name":"Production","scopes":["read","write"]}`,
	)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	key, token := decodeCreatedAPIKey(t, rec)
	require.Equal(t, "Production", key.Name)
	require.Equal(t, []apikeys.Scope{apikeys.ScopeRead, apikeys.ScopeWrite}, key.Scopes)
	require.True(t, strings.HasPrefix(token, "nautilus_"))
	require.Equal(t, token[:15], key.Prefix)

	rec = apiKeyRequest(t, router, actor, http.MethodGet, "/api-keys", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.NotContains(t, rec.Body.String(), token)
	keys := decodeAPIKeys(t, rec)
	require.Len(t, keys, 1)
	require.Equal(t, key.ExternalID, keys[0].ExternalID)

	rec = apiKeyRequest(t, router, actor, http.MethodDelete, "/api-keys/"+key.ExternalID, "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = apiKeyRequest(t, router, actor, http.MethodDelete, "/api-keys/"+key.ExternalID, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), errorsCode("APIKEY-07"))
}

func TestAPIKeyValidationAndDuplicateName(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	router, actor := newTestRouter(t, db, organizations.RoleOwner, "validation")

	rec := apiKeyRequest(t, router, actor, http.MethodPost, "/api-keys", `{"name":"","scopes":[]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), errorsCode("APIKEY-03"))
	require.Contains(t, rec.Body.String(), errorsCode("APIKEY-04"))

	rec = apiKeyRequest(
		t,
		router,
		actor,
		http.MethodPost,
		"/api-keys",
		`{"name":"Production","scopes":["admin"]}`,
	)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), errorsCode("APIKEY-05"))

	body := `{"name":"Production","scopes":["read"]}`
	rec = apiKeyRequest(t, router, actor, http.MethodPost, "/api-keys", body)
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = apiKeyRequest(
		t,
		router,
		actor,
		http.MethodPost,
		"/api-keys",
		`{"name":"production","scopes":["write"]}`,
	)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), errorsCode("APIKEY-06"))
}

func TestAPIKeyPermissionsAndOrganizationScope(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ownerRouter, owner := newTestRouter(t, db, organizations.RoleOwner, "owner")
	adminRouter, admin := newTestRouter(t, db, organizations.RoleAdmin, "admin")
	otherRouter, otherOwner := newTestRouter(t, db, organizations.RoleOwner, "other")
	viewerRouter, viewer := newTestRouter(t, db, organizations.RoleViewer, "viewer")
	body := `{"name":"Production","scopes":["read"]}`

	rec := apiKeyRequest(t, ownerRouter, owner, http.MethodPost, "/api-keys", body)
	require.Equal(t, http.StatusCreated, rec.Code)
	key, _ := decodeCreatedAPIKey(t, rec)
	rec = apiKeyRequest(t, adminRouter, admin, http.MethodPost, "/api-keys", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = apiKeyRequest(t, otherRouter, otherOwner, http.MethodGet, "/api-keys", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, decodeAPIKeys(t, rec))
	rec = apiKeyRequest(t, otherRouter, otherOwner, http.MethodDelete, "/api-keys/"+key.ExternalID, "")
	require.Equal(t, http.StatusNotFound, rec.Code)

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api-keys"},
		{method: http.MethodPost, path: "/api-keys", body: body},
		{method: http.MethodDelete, path: "/api-keys/" + key.ExternalID},
	} {
		rec = apiKeyRequest(t, viewerRouter, viewer, request.method, request.path, request.body)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Contains(t, rec.Body.String(), errorsCode("APIKEY-02"))
	}

	rec = apiKeyRequest(t, ownerRouter, nil, http.MethodGet, "/api-keys", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), errorsCode("APIKEY-01"))
	rec = apiKeyRequest(t, ownerRouter, owner, http.MethodDelete, "/api-keys/not-a-uuid", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

type apiKeyActor struct {
	user         *users.User
	organization *organizations.Organization
	member       *organizations.Member
}

func newTestRouter(
	t *testing.T,
	db database.Database,
	role organizations.Role,
	suffix string,
) (*mux.Router, *apiKeyActor) {
	t.Helper()
	userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "api-key-handler-" + suffix})
	user, err := users.Get(t.Context(), db, userID)
	require.NoError(t, err)
	organizationID := testutil.CreateTestOrg(t, db, "api-key-handler-"+suffix, "API Key Handler "+suffix)
	organization, err := organizations.Get(t.Context(), db, organizationID)
	require.NoError(t, err)
	memberID := testutil.CreateTestOrgMember(t, db, userID, organizationID, role)
	member, err := organizations.GetMember(t.Context(), db, memberID)
	require.NoError(t, err)
	router := mux.New()
	NewMux(db).Mount(router, "/api-keys")
	return router, &apiKeyActor{user: user, organization: organization, member: member}
}

func apiKeyRequest(
	t *testing.T,
	router *mux.Router,
	actor *apiKeyActor,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor != nil {
		req = req.WithContext(apiKeyActorContext(req.Context(), actor))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func apiKeyActorContext(ctx context.Context, actor *apiKeyActor) context.Context {
	ctx = users.WithContext(ctx, actor.user)
	ctx = organizations.WithContext(ctx, actor.organization)
	return organizations.WithMemberContext(ctx, actor.member)
}

func decodeCreatedAPIKey(t *testing.T, rec *httptest.ResponseRecorder) (*apikeys.Key, string) {
	t.Helper()
	var response struct {
		APIKey *apikeys.Key `json:"api_key"`
		Token  string       `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotNil(t, response.APIKey)
	require.NotEmpty(t, response.Token)
	return response.APIKey, response.Token
}

func decodeAPIKeys(t *testing.T, rec *httptest.ResponseRecorder) []*apikeys.Key {
	t.Helper()
	var response struct {
		APIKeys []*apikeys.Key `json:"api_keys"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	return response.APIKeys
}

func errorsCode(code string) string {
	return `"code":"` + code + `"`
}
