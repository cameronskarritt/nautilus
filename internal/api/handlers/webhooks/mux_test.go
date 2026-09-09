package webhooks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/mocks"

	"nautilus/internal/workflows/webhookdelivery"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/handlers/webhooks"
	"nautilus/internal/api/version"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type keys struct{}

func (keys) UserKey(context.Context) ([]byte, error) { return bytes.Repeat([]byte{1}, 32), nil }
func (keys) OrganizationKey(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{2}, 32), nil
}
func request(router http.Handler, token, method, path, body, apiVersion string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Version", apiVersion)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestWebhookBearerScopesAndVersioning(t *testing.T) {
	// Cases exercise mutations and key revocation against a shared isolated fixture.
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "api-hooks", "API Hooks")
	otherID := testutil.CreateTestOrg(t, db, "other-hooks", "Other Hooks")
	token := func(org int, name string, scope apikeys.Scope) string {
		_, value, err := apikeys.Create(t.Context(), db, org, userID, &apikeys.CreateOptions{Name: name, Scopes: []apikeys.Scope{scope}})
		require.NoError(t, err)
		return value
	}
	readToken := token(orgID, "read", apikeys.ScopeRead)
	writeToken := token(orgID, "write", apikeys.ScopeWrite)
	otherRead := token(otherID, "read", apikeys.ScopeRead)
	otherWrite := token(otherID, "write", apikeys.ScopeWrite)
	router := mux.New(mux.Config{Middleware: []mux.Middleware{authentication.RequireAPIKey(db), middleware.OrganizationEncryption(keys{}), version.Middleware}})
	webhooks.Mount(router, db, nil)
	body := `{"name":"Orders","url":"https://example.com/hook","event_types":["document.available"]}`
	rec := request(router, writeToken, http.MethodPost, "/webhooks", body, "2026-01-01")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	var created struct {
		Webhook struct {
			ID string `json:"id"`
		} `json:"webhook"`
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.True(t, strings.HasPrefix(created.Secret, "whsec_"))
	path := "/webhooks/" + created.Webhook.ID
	missingDelivery := path + "/deliveries/" + uuid.NewV4().String()
	for _, tt := range []struct {
		name, token, method, path, body, version, code string
		status                                         int
	}{
		{name: "list", token: readToken, method: http.MethodGet, path: "/webhooks", status: 200},
		{name: "get explicit version", token: readToken, method: http.MethodGet, path: path, version: "2026-01-01", status: 200},
		{name: "list deliveries", token: readToken, method: http.MethodGet, path: path + "/deliveries", status: 200},
		{name: "get missing delivery", token: readToken, method: http.MethodGet, path: missingDelivery, status: 404, code: "HTTP-404"},
		{name: "missing delivery attempts", token: readToken, method: http.MethodGet, path: missingDelivery + "/attempts", status: 404, code: "HTTP-404"},
		{name: "missing bearer", method: http.MethodGet, path: "/webhooks", status: 401, code: "APIKEY-09"},
		{name: "invalid bearer", token: "invalid", method: http.MethodGet, path: "/webhooks", status: 401, code: "APIKEY-09"},
		{name: "unsupported version", token: readToken, method: http.MethodGet, path: path, version: "2099-01-01", status: 400, code: "API-01"},
		{name: "unsupported write version", token: writeToken, method: http.MethodPost, path: "/webhooks", body: body, version: "2099-01-01", status: 400, code: "API-01"},
		{name: "write cannot read", token: writeToken, method: http.MethodGet, path: path, status: 403, code: "APIKEY-10"},
		{name: "write cannot list deliveries", token: writeToken, method: http.MethodGet, path: path + "/deliveries", status: 403, code: "APIKEY-10"},
		{name: "write cannot get delivery", token: writeToken, method: http.MethodGet, path: missingDelivery, status: 403, code: "APIKEY-10"},
		{name: "write cannot list attempts", token: writeToken, method: http.MethodGet, path: missingDelivery + "/attempts", status: 403, code: "APIKEY-10"},
		{name: "read cannot create", token: readToken, method: http.MethodPost, path: "/webhooks", body: body, status: 403, code: "APIKEY-10"},
		{name: "read cannot update", token: readToken, method: http.MethodPatch, path: path, body: `{"enabled":false}`, status: 403, code: "APIKEY-10"},
		{name: "read cannot delete", token: readToken, method: http.MethodDelete, path: path, status: 403, code: "APIKEY-10"},
		{name: "read cannot rotate", token: readToken, method: http.MethodPost, path: path + "/secret/rotate", status: 403, code: "APIKEY-10"},
		{name: "foreign read", token: otherRead, method: http.MethodGet, path: path, status: 404, code: "HTTP-404"},
		{name: "foreign history", token: otherRead, method: http.MethodGet, path: path + "/deliveries", status: 404, code: "HTTP-404"},
		{name: "foreign update", token: otherWrite, method: http.MethodPatch, path: path, body: `{"enabled":false}`, status: 404, code: "HTTP-404"},
		{name: "foreign delete", token: otherWrite, method: http.MethodDelete, path: path, status: 404, code: "HTTP-404"},
		{name: "foreign rotation", token: otherWrite, method: http.MethodPost, path: path + "/secret/rotate", status: 404, code: "HTTP-404"},
		{name: "malformed UUID", token: readToken, method: http.MethodGet, path: "/webhooks/invalid", status: 404},
		{name: "unsupported method", token: writeToken, method: http.MethodPut, path: path, status: 405},
		{name: "read cannot test", token: readToken, method: http.MethodPost, path: path + "/test", status: 403, code: "APIKEY-10"},
		{name: "test unavailable", token: writeToken, method: http.MethodPost, path: path + "/test", status: 503, code: "WEBHOOK-12"},
		{name: "replay not mounted", token: writeToken, method: http.MethodPost, path: missingDelivery + "/replay", status: 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := request(router, tt.token, tt.method, tt.path, tt.body, tt.version)
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.code != "" {
				require.Contains(t, rec.Body.String(), tt.code)
			}
			if tt.status == http.StatusOK {
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				require.NotContains(t, rec.Body.String(), "secret")
			}
			if tt.status == http.StatusForbidden {
				require.Equal(t, `Bearer error="insufficient_scope"`, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
	rec = request(router, writeToken, http.MethodPatch, path, `{"enabled":false}`, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"enabled":false`)
	rec = request(router, writeToken, http.MethodPost, path+"/secret/rotate", "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), created.Secret)
	require.Contains(t, rec.Body.String(), "whsec_")
	rec = request(router, writeToken, http.MethodDelete, path, "", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = request(router, readToken, http.MethodGet, path, "", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NoError(t, organizations.Delete(t.Context(), db, orgID))
	rec = request(router, readToken, http.MethodGet, "/webhooks", "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhookTestUsesWriteScope(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "api-test-hook", "API Test Hook")
	_, token, err := apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{Name: "Write", Scopes: []apikeys.Scope{apikeys.ScopeWrite}})
	require.NoError(t, err)
	workflows := mocks.NewClient(t)
	router := mux.New(mux.Config{Middleware: []mux.Middleware{authentication.RequireAPIKey(db), middleware.OrganizationEncryption(keys{}), version.Middleware}})
	webhooks.Mount(router, db, workflows)
	rec := request(router, token, http.MethodPost, "/webhooks", `{"name":"Test","url":"https://example.com/hook","event_types":["document.available"]}`, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Webhook struct {
			ID string `json:"id"`
		} `json:"webhook"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	var deliveryID string
	workflows.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		input := args.Get(3).(webhookdelivery.TestInput)
		require.Equal(t, orgID, input.OrganizationID)
		require.Equal(t, created.Webhook.ID, input.WebhookID)
		deliveryID = input.DeliveryID
	}).Return(nil, nil).Once()
	rec = request(router, token, http.MethodPost, "/webhooks/"+created.Webhook.ID+"/test", "", "2026-01-01")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Body.String(), deliveryID)
	require.Contains(t, rec.Body.String(), `"status":"pending"`)
}
