package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

type mockRatelimiter struct {
	IdentifyFunc func(context.Context, *http.Request) (string, error)
	CountFunc    func(context.Context, string) (int, time.Duration, error)
	LimitFunc    func(context.Context, string) (int, error)
}

func (m *mockRatelimiter) Identify(ctx context.Context, r *http.Request) (string, error) {
	if m.IdentifyFunc != nil {
		return m.IdentifyFunc(ctx, r)
	}
	return "key", nil
}

func (m *mockRatelimiter) Count(ctx context.Context, key string) (int, time.Duration, error) {
	if m.CountFunc != nil {
		return m.CountFunc(ctx, key)
	}
	return 5, 10 * time.Second, nil
}

func (m *mockRatelimiter) Limit(ctx context.Context, key string) (int, error) {
	if m.LimitFunc != nil {
		return m.LimitFunc(ctx, key)
	}
	return 100, nil
}

func TestRatelimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name      string
		Limiter   *mockRatelimiter
		Status    int
		Called    bool
		Headers   map[string]string
		Retry     string
		ErrorBody bool
	}{
		{
			Name:    "within limit",
			Limiter: &mockRatelimiter{},
			Status:  http.StatusOK,
			Called:  true,
			Headers: map[string]string{
				"X-RateLimit-Limit":     "100",
				"X-RateLimit-Remaining": "95",
				"X-RateLimit-Reset":     "10",
			},
		},
		{
			Name: "count equals limit",
			Limiter: &mockRatelimiter{
				CountFunc: func(context.Context, string) (int, time.Duration, error) {
					return 100, 10 * time.Second, nil
				},
			},
			Status: http.StatusOK,
			Called: true,
			Headers: map[string]string{
				"X-RateLimit-Limit":     "100",
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     "10",
			},
		},
		{
			Name: "over limit",
			Limiter: &mockRatelimiter{
				CountFunc: func(context.Context, string) (int, time.Duration, error) {
					return 101, 30 * time.Second, nil
				},
			},
			Status:    http.StatusTooManyRequests,
			Retry:     "30",
			ErrorBody: true,
		},
		{
			Name: "fractional reset truncates",
			Limiter: &mockRatelimiter{
				CountFunc: func(context.Context, string) (int, time.Duration, error) {
					return 10, 1500 * time.Millisecond, nil
				},
			},
			Status: http.StatusOK,
			Called: true,
			Headers: map[string]string{
				"X-RateLimit-Limit":     "100",
				"X-RateLimit-Remaining": "90",
				"X-RateLimit-Reset":     "1",
			},
		},
		{
			Name: "identify error",
			Limiter: &mockRatelimiter{
				IdentifyFunc: func(context.Context, *http.Request) (string, error) {
					return "", errors.New("identify failed")
				},
			},
			Status:    http.StatusInternalServerError,
			ErrorBody: true,
		},
		{
			Name: "count error",
			Limiter: &mockRatelimiter{
				CountFunc: func(context.Context, string) (int, time.Duration, error) {
					return 0, 0, errors.New("count failed")
				},
			},
			Status:    http.StatusInternalServerError,
			ErrorBody: true,
		},
		{
			Name: "limit error",
			Limiter: &mockRatelimiter{
				LimitFunc: func(context.Context, string) (int, error) {
					return 0, errors.New("limit failed")
				},
			},
			Status:    http.StatusInternalServerError,
			ErrorBody: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			Ratelimit(tt.Limiter)(handler).ServeHTTP(rec, req)

			require.Equal(t, tt.Status, rec.Code)
			require.Equal(t, tt.Called, called)
			require.Equal(t, tt.Retry, rec.Header().Get("Retry-After"))
			for key, value := range tt.Headers {
				require.Equal(t, value, rec.Header().Get(key))
			}
			if tt.ErrorBody {
				require.Contains(t, rec.Body.String(), "message")
			}
		})
	}
}
