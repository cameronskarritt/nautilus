package mcpserver_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mcpserver"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestHandler(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "mcp-handler", "MCP Handler")
	key, token, err := apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{
		Name: "MCP", Scopes: []apikeys.Scope{apikeys.ScopeRead},
	})
	require.NoError(t, err)
	logger := log.New(slog.DiscardHandler)
	server := httptest.NewServer(mcpserver.NewHandler(db, logger))
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
				require.Equal(t, "Bearer", response.Header.Get("WWW-Authenticate"))
				require.JSONEq(t, `{"message":"Authentication required","errors":[{"message":"a valid API key is required","code":"APIKEY-09"}]}`, string(data))
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
	require.Len(t, listed.Tools, 1)
	require.Equal(t, "hello_world", listed.Tools[0].Name)
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
