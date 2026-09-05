package log

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nautilus/internal/testutil/require"
)

func TestColorizedHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := NewColorizedHandler(&buf, slog.LevelDebug).
		WithGroup("api").
		WithAttrs([]slog.Attr{slog.String("request_id", "req-1")})

	record := slog.NewRecord(
		time.Date(2026, 6, 25, 12, 34, 56, 789000000, time.UTC),
		slog.LevelInfo,
		"request completed",
		0,
	)
	record.AddAttrs(slog.Int("status", 200))

	require.NoError(t, handler.Handle(context.Background(), record))

	output := buf.String()
	require.Contains(t, output, "12:34:56.7890")
	require.Contains(t, output, "api: request completed")
	require.Contains(t, output, "request_id")
	require.Contains(t, output, "req-1")
	require.Contains(t, output, "status")
	require.Contains(t, output, "200")
}

func TestLoggerContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	fallback := New(slog.NewTextHandler(&buf, nil))
	require.Equal(t, fallback, FromContext(context.Background(), fallback))

	logger := fallback.With("request_id", "req-1")
	ctx := WithContext(context.Background(), logger)
	require.Equal(t, logger, FromContext(ctx))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ctx)
	req = WithFields(req, "user_id", 42)

	FromContext(req.Context()).Info("handled")

	output := buf.String()
	require.Contains(t, output, "request_id=req-1")
	require.Contains(t, output, "user_id=42")
}
