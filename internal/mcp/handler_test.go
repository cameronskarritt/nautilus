package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/enums"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/config"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/oauth"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestHandler(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "mcp-handler", "MCP Handler")
	key, token, err := apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{
		Name: "MCP", Scopes: []enums.Scope{enums.ScopeRead},
	})
	require.NoError(t, err)
	logger := log.New(slog.DiscardHandler)
	server := httptest.NewServer(NewHandler(db, nil, nil, logger))
	t.Cleanup(server.Close)

	// These cases share the key revoked after the protocol checks below.
	for _, tt := range []struct {
		name, method, token, origin string
		status                      int
	}{
		{name: "JSON response", method: http.MethodPost, token: token, status: http.StatusOK},
		{name: "missing key", method: http.MethodPost, status: http.StatusUnauthorized},
		{name: "invalid key", method: http.MethodPost, token: "invalid", status: http.StatusUnauthorized},
		{name: "cross origin", method: http.MethodPost, token: token, origin: "https://untrusted.example", status: http.StatusForbidden},
		{name: "GET unsupported", method: http.MethodGet, token: token, status: http.StatusMethodNotAllowed},
		{name: "DELETE unsupported", method: http.MethodDelete, token: token, status: http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hello_world","arguments":{}}}`)
			req, err := http.NewRequestWithContext(t.Context(), tt.method, server.URL+"/mcp", body)
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			response, err := server.Client().Do(req)
			require.NoError(t, err)
			defer response.Body.Close()
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, tt.status, response.StatusCode)
			switch tt.status {
			case http.StatusOK:
				require.Equal(t, "application/json", response.Header.Get("Content-Type"))
				require.Empty(t, response.Header.Get("Mcp-Session-Id"))
				require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Hello, world!"}]}}`, string(data))
			case http.StatusUnauthorized:
				require.Contains(t, response.Header.Get("WWW-Authenticate"), `resource_metadata="`)
				require.JSONEq(t, `{"message":"Authentication required","errors":[{"message":"a valid MCP access token or API key is required","code":"MCP-01"}]}`, string(data))
			case http.StatusMethodNotAllowed:
				require.Equal(t, "POST", response.Header.Get("Allow"))
			}
		})
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{
			token: token, base: server.Client().Transport,
		}},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, listed.Tools, 5)
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		require.True(t, tool.Annotations.ReadOnlyHint)
	}
	require.ElementsMatch(t, []string{"hello_world", "list_documents", "get_document", "read_document", "download_document"}, names)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "hello_world", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	require.Equal(t, &mcp.TextContent{Text: "Hello, world!"}, result.Content[0])
	require.Nil(t, result.StructuredContent)

	revoked, err := apikeys.RevokeByExternalID(t.Context(), db, orgID, key.ExternalID)
	require.NoError(t, err)
	require.True(t, revoked)
	_, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "hello_world", Arguments: map[string]any{}})
	require.Error(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.Equal(t, "Bearer", response.Header.Get("WWW-Authenticate"))
	require.JSONEq(t, `{"message":"Authentication required","errors":[{"message":"a valid API key is required","code":"APIKEY-09"}]}`, string(data))
}

func TestOAuthHandler(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "mcp-oauth", "MCP OAuth")
	memberID := testutil.CreateTestOrgMember(t, db, userID, orgID, enums.RoleMember)
	client, err := oauth.RegisterClient(t.Context(), db, "Test", []string{"http://localhost/callback"})
	require.NoError(t, err)
	verifier := strings.Repeat("a", 43)
	challenge := sha256.Sum256([]byte(verifier))
	resource := strings.TrimRight(config.Get("MCP_BASE_URL", "http://localhost:8082"), "/") + "/mcp"
	code, err := oauth.CreateGrant(t.Context(), db, userID, memberID, &oauth.GrantOptions{
		ClientID: client.ID, RedirectURI: client.RedirectURIs[0], Resource: resource,
		Scope: "read", Challenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
	})
	require.NoError(t, err)
	tokens, err := oauth.ExchangeCode(t.Context(), db, client.ID, code, client.RedirectURIs[0], resource, verifier)
	require.NoError(t, err)
	server := httptest.NewServer(NewHandler(db, nil, nil, log.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "OAuth test"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{token: tokens.AccessToken, base: server.Client().Transport}},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "hello_world", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.Equal(t, &mcp.TextContent{Text: "Hello, world!"}, result.Content[0])

	response, err := server.Client().Get(server.URL + "/.well-known/oauth-protected-resource/mcp")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var metadata struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&metadata))
	require.Equal(t, resource, metadata.Resource)
	require.Equal(t, []string{strings.TrimSuffix(resource, "/mcp")}, metadata.Servers)
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (transport bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+transport.token)
	response, err := transport.base.RoundTrip(req)
	return response, errors.Wrap(err, "MCP test request failed")
}
