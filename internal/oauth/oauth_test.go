package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"nautilus/internal/config"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
	"nautilus/internal/oauth"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestOAuthFlow(t *testing.T) {
	db := testutil.SetupTestDB(t)
	router := mux.New()
	oauth.NewMux(db).Mount(router, "/mcp/oauth")
	send := func(method, path, contentType, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		app, _ := url.Parse(config.Get("APP_BASE_URL", "http://localhost:5173"))
		req.Header.Set("Origin", app.Scheme+"://"+app.Host)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	rec := send(http.MethodPost, "/mcp/oauth/register", "application/json", `{"client_name":"Test client","redirect_uris":["http://127.0.0.1:43123/callback"]}`, nil)
	require.Equal(t, 201, rec.Code)
	var client struct {
		ID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &client))
	require.NotEmpty(t, client.ID)
	verifier := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	params := url.Values{"client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:43123/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}, "scope": {"read write read"}, "resource": {oauth.Resource()}, "state": {"opaque state"}}
	rec = send(http.MethodGet, "/mcp/oauth/authorize?"+params.Encode(), "", "", nil)
	require.Equal(t, 302, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "/mcp/authorize?")
	for _, invalid := range []struct{ key, value string }{
		{"redirect_uri", "https://evil.test/callback"},
		{"code_challenge_method", "plain"},
		{"code_challenge", "invalid"},
		{"resource", "https://evil.test/mcp"},
		{"scope", "admin"},
	} {
		v, err := url.ParseQuery(params.Encode())
		require.NoError(t, err)
		v.Set(invalid.key, invalid.value)
		rec = send(http.MethodGet, "/mcp/oauth/authorize?"+v.Encode(), "", "", nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Empty(t, rec.Header().Get("Location"))
	}
	rec = send(http.MethodGet, "/mcp/oauth/request?"+params.Encode(), "", "", nil)
	require.Equal(t, 401, rec.Code)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "oauth-flow", "OAuth Flow")
	memberID := testutil.CreateTestOrgMember(t, db, userID, orgID, enums.RoleOwner)
	session, err := sessions.Create(t.Context(), db, userID, optional.Set(memberID), nil)
	require.NoError(t, err)
	cookie := sessions.CreateCookie(session.Token)
	rec = send(http.MethodGet, "/mcp/oauth/request?"+params.Encode(), "", "", cookie)
	require.Equal(t, 200, rec.Code)
	var consent struct {
		Scope        string `json:"scope"`
		UserID       string `json:"user_id"`
		Organization struct {
			ID string `json:"id"`
		} `json:"organization"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &consent))
	require.Equal(t, "read write", consent.Scope)
	body := map[string]string{}
	for k, v := range params {
		body[k] = v[0]
	}
	body["decision"] = "allow"
	body["user_id"] = consent.UserID
	body["organization_id"] = consent.Organization.ID
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	rec = send(http.MethodPost, "/mcp/oauth/authorize", "text/plain", string(raw), cookie)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = send(http.MethodPost, "/mcp/oauth/authorize", "application/json", string(raw), cookie)
	require.Equal(t, 200, rec.Code)
	var granted struct {
		URL string `json:"redirect_url"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &granted))
	target, err := url.Parse(granted.URL)
	require.NoError(t, err)
	require.Equal(t, "opaque state", target.Query().Get("state"))
	require.NotEmpty(t, target.Query().Get("code"))
	tokenParams := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "redirect_uri": {params.Get("redirect_uri")}, "resource": {oauth.Resource()}, "code": {target.Query().Get("code")}, "code_verifier": {verifier}}
	rec = send(http.MethodPost, "/mcp/oauth/token", "application/x-www-form-urlencoded", tokenParams.Encode(), nil)
	require.Equal(t, 200, rec.Code)
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tokens))
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {tokens.RefreshToken}}
	for _, resource := range []string{"https://evil.test/mcp", ""} {
		refresh.Set("resource", resource)
		rec = send(http.MethodPost, "/mcp/oauth/token", "application/x-www-form-urlencoded", refresh.Encode(), nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.JSONEq(t, `{"error":"invalid_target"}`, rec.Body.String())
	}
	refresh.Del("resource")
	rec = send(http.MethodPost, "/mcp/oauth/token", "application/x-www-form-urlencoded", refresh.Encode(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	previousRefresh := tokens.RefreshToken
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tokens))
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	require.NotEqual(t, previousRefresh, tokens.RefreshToken)
	rec = send(http.MethodPost, "/mcp/oauth/token", "application/x-www-form-urlencoded", tokenParams.Encode(), nil)
	require.Equal(t, 400, rec.Code)
	require.JSONEq(t, `{"error":"invalid_grant"}`, rec.Body.String())
	body["decision"] = "deny"
	raw, err = json.Marshal(body)
	require.NoError(t, err)
	rec = send(http.MethodPost, "/mcp/oauth/authorize", "application/json", string(raw), cookie)
	require.Equal(t, 200, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &granted))
	target, err = url.Parse(granted.URL)
	require.NoError(t, err)
	require.Equal(t, "access_denied", target.Query().Get("error"))
	require.Empty(t, target.Query().Get("code"))
	body["organization_id"] = "another-org"
	raw, err = json.Marshal(body)
	require.NoError(t, err)
	rec = send(http.MethodPost, "/mcp/oauth/authorize", "application/json", string(raw), cookie)
	require.Equal(t, 403, rec.Code)
	user, err := users.Get(t.Context(), db, userID)
	require.NoError(t, err)
	require.Equal(t, user.ExternalID, consent.UserID)
	stored, err := sessions.Get(t.Context(), db, session.Token)
	require.NoError(t, err)
	require.NoError(t, sessions.AssumeOrg(t.Context(), db, stored.ID, orgID))
	rec = send(http.MethodGet, "/mcp/oauth/request?"+params.Encode(), "", "", cookie)
	require.Equal(t, http.StatusForbidden, rec.Code)

}

func TestRegistrationRejectsUnsafeRedirects(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{"http://example.com/callback", "https://user:password@example.com/callback", "https://example.com/#fragment", "javascript:alert(1)", "https://example.com/#", "http://127.0.0.1.evil.test/callback"} {
		t.Run(uri, func(t *testing.T) {
			t.Parallel()
			body, err := json.Marshal(map[string]any{"redirect_uris": []string{uri}})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			oauth.NewMux(nil).Register(rec, req)
			require.Equal(t, 400, rec.Code)
			require.JSONEq(t, `{"error":"invalid_redirect_uri"}`, rec.Body.String())
		})
	}
}
func TestConsentRejectsCrossOrigin(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"", "https://evil.test", "null"} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			oauth.NewMux(nil).Consent(rec, req)
			require.Equal(t, 403, rec.Code)
			require.JSONEq(t, `{"error":"access_denied"}`, rec.Body.String())
		})
	}
}
func TestTokenRejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, contentType, body, code string }{
		{"json", "application/json", `{}`, "invalid_request"},
		{"duplicate parameter", "application/x-www-form-urlencoded", "client_id=a&client_id=b", "invalid_request"},
		{"secret", "application/x-www-form-urlencoded", "client_id=a&client_secret=b", "invalid_client"},
		{"wrong resource", "application/x-www-form-urlencoded", "client_id=a&resource=https://evil.test", "invalid_target"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			oauth.NewMux(nil).Token(rec, req)
			require.Equal(t, 400, rec.Code)
			require.JSONEq(t, `{"error":"`+tt.code+`"}`, rec.Body.String())
		})
	}
}
