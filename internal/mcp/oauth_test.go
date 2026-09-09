package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"nautilus/internal/config"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/oauth"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestSDKOAuth(t *testing.T) {
	// Config is process-global; restore its cache after t.Setenv restores values.
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "sdk-oauth", "SDK OAuth")
	member, err := organizations.CreateMember(t.Context(), db, userID, orgID, enums.RoleMember, optional.Empty[string]())
	require.NoError(t, err)
	session, err := sessions.Create(t.Context(), db, userID, optional.Set(member.ID), nil)
	require.NoError(t, err)
	router := mux.New()
	oauth.NewMux(db).Mount(router, "/mcp/oauth")
	api := httptest.NewServer(router)
	t.Cleanup(api.Close)
	server := httptest.NewUnstartedServer(nil)
	t.Cleanup(server.Close)
	const appURL = "https://app.example"
	t.Setenv("MCP_BASE_URL", "http://"+server.Listener.Addr().String())
	t.Setenv("API_BASE_URL", api.URL)
	t.Setenv("APP_BASE_URL", appURL)
	config.SetProvider(new(config.EnvProvider))
	server.Config.Handler = NewHandler(db, nil, nil, log.New(slog.DiscardHandler))
	server.Start()

	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	consentJSON := func(ctx context.Context, method, target string, body []byte, result any) error {
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
		if err != nil {
			return errors.Wrap(err, "unable to create consent request")
		}
		req.AddCookie(sessions.CreateCookie(session.Token))
		req.Header.Set("Origin", appURL)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := browser.Do(req)
		if err != nil {
			return errors.Wrap(err, "unable to request consent")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return errors.Errorf("consent request returned HTTP %d", response.StatusCode)
		}
		return errors.Wrap(json.NewDecoder(response.Body).Decode(result), "unable to decode consent response")
	}
	var initial oauth2.Token
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "SDK integration", RedirectURIs: []string{"http://127.0.0.1:43123/callback"},
			TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"},
		}},
		RequestRefreshToken: true,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
			if err != nil {
				return nil, errors.Wrap(err, "unable to create authorization request")
			}
			response, err := browser.Do(req)
			if err != nil {
				return nil, errors.Wrap(err, "unable to request authorization")
			}
			response.Body.Close()
			if response.StatusCode != http.StatusFound {
				return nil, errors.Errorf("authorization request returned HTTP %d", response.StatusCode)
			}
			location, err := response.Location()
			if err != nil {
				return nil, errors.Wrap(err, "unable to read consent location")
			}
			if location.Scheme+"://"+location.Host != appURL || location.Path != "/mcp/authorize" {
				return nil, errors.New("authorization did not redirect to consent page")
			}
			var consent struct {
				UserID       string              `json:"user_id"`
				Organization struct{ ID string } `json:"organization"`
			}
			if err := consentJSON(ctx, http.MethodGet, api.URL+"/mcp/oauth/request?"+location.RawQuery, nil, &consent); err != nil {
				return nil, err
			}
			body := map[string]string{"decision": "allow", "user_id": consent.UserID, "organization_id": consent.Organization.ID}
			for key, values := range location.Query() {
				body[key] = values[0]
			}
			data, err := json.Marshal(body)
			if err != nil {
				return nil, errors.Wrap(err, "unable to encode consent")
			}
			var granted struct {
				URL string `json:"redirect_url"`
			}
			if err := consentJSON(ctx, http.MethodPost, api.URL+"/mcp/oauth/authorize", data, &granted); err != nil {
				return nil, err
			}
			target, err := url.Parse(granted.URL)
			if err != nil {
				return nil, errors.Wrap(err, "unable to parse client redirect")
			}
			return &auth.AuthorizationResult{Code: target.Query().Get("code"), State: target.Query().Get("state")}, nil
		},
		NewTokenSource: func(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			initial = *token
			token.Expiry = time.Now().Add(-time.Hour)
			return cfg.TokenSource(ctx, token), nil
		},
	})
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	connected, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", OAuthHandler: handler,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connected.Close() })
	result, err := connected.CallTool(t.Context(), &mcp.CallToolParams{Name: "hello_world", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, []mcp.Content{&mcp.TextContent{Text: "Hello, world!"}}, result.Content)
	source, err := handler.TokenSource(t.Context())
	require.NoError(t, err)
	fresh, err := source.Token()
	require.NoError(t, err)
	require.NotEmpty(t, initial.AccessToken)
	require.NotEmpty(t, initial.RefreshToken)
	require.NotEqual(t, initial.AccessToken, fresh.AccessToken)
	require.NotEqual(t, initial.RefreshToken, fresh.RefreshToken)
	require.True(t, fresh.Valid())
}
