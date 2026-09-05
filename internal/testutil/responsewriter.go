package testutil

import (
	"bufio"
	"net"
	"net/http/httptest"

	"nautilus/internal/errors"
)

// TestResponseWriter implements both http.Flusher and http.Hijacker for testing.
// It wraps httptest.ResponseRecorder to provide the interfaces needed by middleware
// that require Flusher and Hijacker implementations.
type TestResponseWriter struct {
	*httptest.ResponseRecorder
}

func (t *TestResponseWriter) Flush() {
	t.ResponseRecorder.Flush()
}

func (t *TestResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijacking not supported in test")
}

func NewTestResponseWriter() *TestResponseWriter {
	return &TestResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}
