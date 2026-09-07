package authentication

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRequireAPIKeyAuthenticatesAndAddsSafeLogContext(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "api-auth"})
	organizationID := testutil.CreateTestOrg(t, db, "api-auth", "API Auth")
	key, token, err := apikeys.Create(t.Context(), db, organizationID, userID, &apikeys.CreateOptions{
		Name:   "Production",
		Scopes: []apikeys.Scope{apikeys.ScopeRead},
	})
	require.NoError(t, err)

	var logs bytes.Buffer
	logger := log.New(slog.NewJSONHandler(&logs, nil))
	handler := RequireAPIKey(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated := apikeys.FromContext(r.Context())
		require.NotNil(t, authenticated)
		require.Equal(t, key.ExternalID, authenticated.ExternalID)
		org := organizations.FromContext(r.Context())
		require.NotNil(t, org)
		require.Equal(t, organizationID, org.ID)
		require.NotEmpty(t, org.ExternalID)
		log.FromContext(r.Context()).Info("authenticated request")
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "bearer  "+token)
	req = req.WithContext(log.WithContext(req.Context(), logger))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Contains(t, logs.String(), `"api_key_id":`+strconv.Itoa(key.ID))
	require.Contains(t, logs.String(), `"organization_id":`+strconv.Itoa(organizationID))
	require.NotContains(t, logs.String(), token)
}

func TestRequireAPIKeyRejectsDeletedOrganization(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, t.Name(), "Organization")
	_, token, err := apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{
		Name: "Key", Scopes: []apikeys.Scope{apikeys.ScopeRead},
	})
	require.NoError(t, err)
	require.NoError(t, organizations.Delete(t.Context(), db, orgID))
	router := mux.New()
	router.Use(RequireAPIKey(db))
	router.Get("/protected", func(http.ResponseWriter, *http.Request) {
		t.Fatal("key belonging to deleted organization reached handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
	require.JSONEq(t, `{"message":"Authentication required","errors":[{"message":"a valid API key is required","code":"APIKEY-09"}]}`, rec.Body.String())
}

func TestRequireAPIKeyUsesOneUnauthorizedResponse(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "api-auth-revoke"})
	organizationID := testutil.CreateTestOrg(t, db, "api-auth-revoke", "API Auth Revoke")
	key, token, err := apikeys.Create(t.Context(), db, organizationID, userID, &apikeys.CreateOptions{
		Name:   "Production",
		Scopes: []apikeys.Scope{apikeys.ScopeRead},
	})
	require.NoError(t, err)

	handler := RequireAPIKey(db)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unauthenticated request reached handler")
	}))
	headers := []string{
		"",
		"Basic credentials",
		"Bearer",
		"Bearer nautilus_short",
		"Bearer nautilus_0123456789012345678901234567890123456789012",
	}
	var responseBody string
	for _, header := range headers {
		rec := serveAuthentication(handler, header)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
		if responseBody == "" {
			responseBody = rec.Body.String()
		} else {
			require.JSONEq(t, responseBody, rec.Body.String())
		}
	}

	revoked, err := apikeys.RevokeByExternalID(t.Context(), db, organizationID, key.ExternalID)
	require.NoError(t, err)
	require.True(t, revoked)
	rec := serveAuthentication(handler, "Bearer "+token)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, responseBody, rec.Body.String())

	var response struct {
		Errors []errors.ErrorDetail `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Errors, 1)
	require.Equal(t, errors.ErrorCode(errors.ErrorCodeAPIKEY09), response.Errors[0].Code)
}

func TestRequireScopes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		key        *apikeys.Key
		required   []apikeys.Scope
		wantStatus int
		wantCode   errors.ErrorCode
	}{
		{
			name:       "read scope",
			key:        &apikeys.Key{Scopes: []apikeys.Scope{apikeys.ScopeRead}},
			required:   []apikeys.Scope{apikeys.ScopeRead},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "all required scopes",
			key:        &apikeys.Key{Scopes: []apikeys.Scope{apikeys.ScopeRead, apikeys.ScopeWrite}},
			required:   []apikeys.Scope{apikeys.ScopeRead, apikeys.ScopeWrite},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "missing required scope",
			key:        &apikeys.Key{Scopes: []apikeys.Scope{apikeys.ScopeRead}},
			required:   []apikeys.Scope{apikeys.ScopeWrite},
			wantStatus: http.StatusForbidden,
			wantCode:   errors.ErrorCodeAPIKEY10,
		},
		{
			name:       "missing authenticated key",
			required:   []apikeys.Scope{apikeys.ScopeRead},
			wantStatus: http.StatusUnauthorized,
			wantCode:   errors.ErrorCodeAPIKEY09,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := RequireScopes(tt.required...)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.key != nil {
				req = req.WithContext(apikeys.WithContext(req.Context(), tt.key))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantCode != "" {
				var response struct {
					Errors []errors.ErrorDetail `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				require.Len(t, response.Errors, 1)
				require.Equal(t, tt.wantCode, response.Errors[0].Code)
			}
		})
	}
}

func serveAuthentication(handler http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
