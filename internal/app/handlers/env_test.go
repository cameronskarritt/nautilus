package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/app/handlers"
	"nautilus/internal/testutil/require"
)

func TestEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name         string
		SSOProviders []string
		Expected     handlers.EnvResponse
	}{
		{
			Name:         "configured",
			SSOProviders: []string{"google", "github"},
			Expected: handlers.EnvResponse{
				Version: 1,
				Auth: handlers.EnvAuth{
					SSOProviders: []string{"github", "google"},
				},
			},
		},
		{
			Name: "unconfigured",
			Expected: handlers.EnvResponse{
				Version: 1,
				Auth: handlers.EnvAuth{
					SSOProviders: []string{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/env", nil)
			rec := httptest.NewRecorder()
			handlers.Env(tt.SSOProviders).ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "public, max-age=60", rec.Header().Get("Cache-Control"))

			var response handlers.EnvResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, tt.Expected, response)
		})
	}
}
