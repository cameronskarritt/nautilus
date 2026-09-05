package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/observability/stacktrace"
	"nautilus/internal/testutil/require"
)

func TestRecover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name   string
		Panic  any
		Status int
		Body   string
	}{
		{Name: "error panic", Panic: errors.New("boom"), Status: http.StatusInternalServerError},
		{Name: "string panic", Panic: "boom", Status: http.StatusInternalServerError},
		{Name: "other panic", Panic: 42, Status: http.StatusInternalServerError},
		{Name: "no panic", Status: http.StatusOK, Body: "success"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.Panic != nil {
					panic(tt.Panic)
				}
				_, _ = w.Write([]byte(tt.Body))
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(log.WithContext(context.Background(), log.Default()))
			rec := httptest.NewRecorder()

			Recover(handler).ServeHTTP(rec, req)

			require.Equal(t, tt.Status, rec.Code)
			if tt.Body != "" {
				require.Equal(t, tt.Body, rec.Body.String())
				return
			}
			require.Contains(t, rec.Body.String(), "Unable to handle request")
		})
	}
}

func TestRecoverCapturesPanic(t *testing.T) {
	t.Parallel()

	tracer := &recordingStackTracer{}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	req := httptest.NewRequest(http.MethodPost, "/panic", nil)
	ctx := stacktrace.WithContext(log.WithContext(context.Background(), log.Default()), tracer)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	Recover(handler).ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotNil(t, tracer.err)
	require.Contains(t, tracer.err.Error(), "boom")
	require.NotNil(t, tracer.opts)
	require.Equal(t, req, tracer.opts.Request)
}

type recordingStackTracer struct {
	err  error
	opts *stacktrace.CaptureOptions
}

func (t *recordingStackTracer) Capture(_ context.Context, err error, opts *stacktrace.CaptureOptions) {
	t.err = err
	t.opts = opts
}
