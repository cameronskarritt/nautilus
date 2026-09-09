package middleware_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/oauth"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestMCPAuth(t *testing.T) {
	const issuer = "https://mcp.example"
	db := testutil.SetupTestDB(t)
	keyUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "mcp-key"})
	keyOrgID := testutil.CreateTestOrg(t, db, "mcp-key", "API Key Organization")
	key, apiToken, err := apikeys.Create(t.Context(), db, keyOrgID, keyUserID, &apikeys.CreateOptions{
		Name: "MCP", Scopes: []apikeys.Scope{apikeys.ScopeRead},
	})
	require.NoError(t, err)
	userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "mcp-oauth"})
	orgID := testutil.CreateTestOrg(t, db, "mcp-oauth", "OAuth Organization")
	member, err := organizations.CreateMember(t.Context(), db, userID, orgID, enums.RoleMember, optional.Empty[string]())
	require.NoError(t, err)
	client, err := oauth.RegisterClient(t.Context(), db, "MCP Test", []string{"http://localhost/callback"})
	require.NoError(t, err)
	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	code, err := oauth.CreateGrant(t.Context(), db, userID, member.ID, &oauth.GrantOptions{
		ClientID: client.ID, RedirectURI: client.RedirectURIs[0], Resource: issuer + "/mcp",
		Scope: "read", Challenge: base64.RawURLEncoding.EncodeToString(digest[:]),
	})
	require.NoError(t, err)
	tokens, err := oauth.ExchangeCode(t.Context(), db, client.ID, code, client.RedirectURIs[0], issuer+"/mcp", verifier)
	require.NoError(t, err)
	session, err := sessions.Create(t.Context(), db, userID, optional.Set(member.ID), nil)
	require.NoError(t, err)
	bearer := "Bearer " + tokens.AccessToken
	legacy := "Bearer " + apiToken

	// Cases share credentials that are revoked after the precedence checks.
	for _, tt := range []struct {
		name, principal, code string
		keys, authorization   []string
		cookie                bool
	}{
		{name: "API key header", keys: []string{apiToken}, principal: "key"},
		{name: "legacy API key", authorization: []string{legacy}, principal: "key"},
		{name: "OAuth", authorization: []string{bearer}, principal: "oauth"},
		{name: "API key wins over OAuth", keys: []string{apiToken}, authorization: []string{bearer}, principal: "key"},
		{name: "API key wins over duplicate Authorization", keys: []string{apiToken}, authorization: []string{bearer, bearer}, principal: "key"},
		{name: "invalid API key wins over OAuth", keys: []string{"invalid"}, authorization: []string{bearer}, code: "APIKEY-09"},
		{name: "empty API key wins over OAuth", keys: []string{""}, authorization: []string{bearer}, code: "APIKEY-09"},
		{name: "duplicate API key", keys: []string{apiToken, apiToken}, authorization: []string{bearer}, code: "APIKEY-09"},
		{name: "invalid legacy API key", authorization: []string{"Bearer nautilus_invalid"}, code: "APIKEY-09"},
		{name: "missing credentials", code: "MCP-01"},
		{name: "browser cookie rejected", cookie: true, code: "MCP-01"},
		{name: "invalid OAuth", authorization: []string{"Bearer invalid"}, code: "MCP-01"},
		{name: "invalid scheme", authorization: []string{"Basic invalid"}, code: "MCP-01"},
		{name: "duplicate OAuth", authorization: []string{bearer, bearer}, code: "MCP-01"},
		{name: "duplicate mixed bearer", authorization: []string{legacy, bearer}, code: "MCP-01"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := middleware.MCPAuth(db, issuer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				ctx := r.Context()
				require.Equal(t, -1, sessions.FromContext(ctx))
				org := organizations.FromContext(ctx)
				require.NotNil(t, org)
				switch tt.principal {
				case "key":
					require.Equal(t, keyOrgID, org.ID)
					authenticated := apikeys.FromContext(ctx)
					require.NotNil(t, authenticated)
					require.Equal(t, key.ID, authenticated.ID)
					require.Nil(t, oauth.FromContext(ctx))
					require.Nil(t, users.FromContext(ctx))
					require.Nil(t, organizations.MemberFromContext(ctx))
				case "oauth":
					require.Equal(t, orgID, org.ID)
					grant := oauth.FromContext(ctx)
					require.NotNil(t, grant)
					require.Equal(t, client.ID, grant.ClientID)
					require.Equal(t, userID, grant.UserID)
					require.Equal(t, member.ID, grant.MemberID)
					require.Equal(t, issuer+"/mcp", grant.Resource)
					require.Equal(t, "read", grant.Scope)
					authenticated := users.FromContext(ctx)
					require.NotNil(t, authenticated)
					require.Equal(t, userID, authenticated.ID)
					membership := organizations.MemberFromContext(ctx)
					require.NotNil(t, membership)
					require.Equal(t, member.ID, membership.ID)
					require.Nil(t, apikeys.FromContext(ctx))
				default:
					t.Fatal("unauthenticated request reached handler")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodPost, issuer+"/mcp", nil)
			if tt.keys != nil {
				req.Header["X-Api-Key"] = tt.keys
			}
			if tt.authorization != nil {
				req.Header["Authorization"] = tt.authorization
			}
			if tt.cookie {
				req.AddCookie(sessions.CreateCookie(session.Token))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, tt.authorization, req.Header.Values("Authorization"))
			if tt.code == "" {
				require.True(t, called)
				require.Equal(t, http.StatusNoContent, rec.Code)
				return
			}
			require.False(t, called)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			var response struct {
				Errors []errors.ErrorDetail `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Len(t, response.Errors, 1)
			require.Equal(t, errors.ErrorCode(tt.code), response.Errors[0].Code)
			challenge := rec.Header().Get("WWW-Authenticate")
			if tt.code == "APIKEY-09" {
				require.Equal(t, "Bearer", challenge)
				return
			}
			require.Contains(t, challenge, `Bearer resource_metadata="`+issuer+`/.well-known/oauth-protected-resource/mcp"`)
			if len(tt.authorization) > 0 {
				require.Contains(t, challenge, `error="invalid_token"`)
			} else {
				require.NotContains(t, challenge, "invalid_token")
			}
		})
	}

	require.NoError(t, oauth.Revoke(t.Context(), db, client.ID, tokens.AccessToken))
	for _, withKey := range []bool{false, true} {
		handler := middleware.MCPAuth(db, issuer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.True(t, withKey)
			require.Equal(t, keyOrgID, organizations.FromContext(r.Context()).ID)
			require.Nil(t, oauth.FromContext(r.Context()))
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(http.MethodPost, issuer+"/mcp", nil)
		req.Header.Set("Authorization", bearer)
		if withKey {
			req.Header.Set("X-API-Key", apiToken)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if withKey {
			require.Equal(t, http.StatusNoContent, rec.Code)
		} else {
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Contains(t, rec.Body.String(), "MCP-01")
			require.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
		}
	}
}
