package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/log"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type testLogHandler struct {
	Records *[]slog.Record
	Attrs   []slog.Attr
}

func (h *testLogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	record := r.Clone()
	record.AddAttrs(h.Attrs...)
	*h.Records = append(*h.Records, record)
	return nil
}

func (h *testLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &testLogHandler{Records: h.Records}
	next.Attrs = append(append([]slog.Attr{}, h.Attrs...), attrs...)
	return next
}

func (h *testLogHandler) WithGroup(string) slog.Handler {
	return h
}

func TestAccessLog(t *testing.T) {
	t.Parallel()

	records := make([]slog.Record, 0)
	logs := &testLogHandler{Records: &records}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})
	req := httptest.NewRequest(http.MethodPost, "/api/users?page=1", nil)
	req = req.WithContext(log.WithContext(context.Background(), log.New(logs)))
	rec := testutil.NewTestResponseWriter()

	AccessLog(handler).ServeHTTP(rec, req)

	requestID := rec.Header().Get("Request-Id")
	require.Len(t, requestID, 8)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "created", rec.Body.String())
	require.Len(t, records, 2)

	require.Equal(t, "request started", records[0].Message)
	started := logAttrs(records[0])
	require.Equal(t, requestID, started["request_id"])
	require.Equal(t, http.MethodPost, started["method"])
	require.Equal(t, "/api/users?page=1", started["url"])

	require.Equal(t, "request completed", records[1].Message)
	completed := logAttrs(records[1])
	require.Contains(t, completed, "duration")
	require.Equal(t, int64(http.StatusCreated), completed["status"])
	require.Equal(t, int64(len("created")), completed["size"])
}

func TestWrapWriter(t *testing.T) {
	t.Parallel()

	t.Run("wraps writer", func(t *testing.T) {
		t.Parallel()

		w := testutil.NewTestResponseWriter()
		ww, err := WrapWriter(w)
		require.NoError(t, err)
		require.Equal(t, 0, ww.Status())
		require.Equal(t, 0, ww.BytesWritten())
		require.False(t, ww.Hijacked())
		require.Equal(t, w, ww.Unwrap())
	})

	t.Run("requires flusher", func(t *testing.T) {
		t.Parallel()

		ww, err := WrapWriter(struct{ http.ResponseWriter }{httptest.NewRecorder()})
		require.Error(t, err)
		require.Nil(t, ww)
		require.Contains(t, err.Error(), "not a Flusher")
	})

	t.Run("requires hijacker", func(t *testing.T) {
		t.Parallel()

		ww, err := WrapWriter(httptest.NewRecorder())
		require.Error(t, err)
		require.Nil(t, ww)
		require.Contains(t, err.Error(), "not a Hijacker")
	})
}

func TestWriterProxy(t *testing.T) {
	t.Parallel()

	w := testutil.NewTestResponseWriter()
	ww, err := WrapWriter(w)
	require.NoError(t, err)

	tee := &bytes.Buffer{}
	ww.Tee(tee)
	n, err := ww.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, len("hello"), n)
	require.Equal(t, http.StatusOK, ww.Status())
	require.Equal(t, 5, ww.BytesWritten())
	require.Equal(t, "hello", w.Body.String())
	require.Equal(t, "hello", tee.String())

	ww.WriteHeader(http.StatusCreated)
	require.Equal(t, http.StatusOK, ww.Status())

	ww.Tee(nil)
	_, err = ww.Write([]byte("!"))
	require.NoError(t, err)
	require.Equal(t, 6, ww.BytesWritten())
	require.Equal(t, "hello", tee.String())
}

func TestWriterProxy_Hijack(t *testing.T) {
	t.Parallel()

	w := testutil.NewTestResponseWriter()
	ww, err := WrapWriter(w)
	require.NoError(t, err)

	_, _, err = ww.Hijack()
	require.Error(t, err)
	require.True(t, ww.Hijacked())
}

func TestRandomString(t *testing.T) {
	t.Parallel()

	str := randomString(100)
	require.Len(t, str, 100)
	for _, char := range str {
		require.Contains(t, alphabet, string(char))
	}
}

func logAttrs(record slog.Record) map[string]any {
	attrs := make(map[string]any)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	return attrs
}
