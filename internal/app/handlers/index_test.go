package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/app/handlers"
	"nautilus/internal/testutil/require"
)

func TestHealthcheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name           string
		ServerContext  func() context.Context
		ExpectedCode   int
		ExpectedStatus string
	}{
		{
			Name: "returns ok when server context is active",
			ServerContext: func() context.Context {
				return context.Background()
			},
			ExpectedCode:   http.StatusOK,
			ExpectedStatus: "ok",
		},
		{
			Name: "returns service unavailable when server context is canceled",
			ServerContext: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			ExpectedCode:   http.StatusServiceUnavailable,
			ExpectedStatus: "shutting_down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()

			handlers.Healthcheck(tt.ServerContext())(rec, req)

			require.Equal(t, tt.ExpectedCode, rec.Code)

			var body map[string]string
			err := json.Unmarshal(rec.Body.Bytes(), &body)
			require.NoError(t, err)
			require.Equal(t, tt.ExpectedStatus, body["status"])
		})
	}
}
