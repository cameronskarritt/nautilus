package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/observability/tracer"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type mockSpan struct {
	Attributes []tracer.Attribute
	Status     tracer.Status
	StatusDesc string
	Ended      bool
}

func (m *mockSpan) SetAttributes(attrs ...tracer.Attribute) {
	m.Attributes = append(m.Attributes, attrs...)
}

func (m *mockSpan) RecordError(error) {}

func (m *mockSpan) AddEvent(string, ...tracer.EventOption) {}

func (m *mockSpan) SetStatus(status tracer.Status, description string) {
	m.Status = status
	m.StatusDesc = description
}

func (m *mockSpan) End(...tracer.EndOption) {
	m.Ended = true
}

type mockTracer struct {
	Name string
	Span *mockSpan
}

func (m *mockTracer) Start(ctx context.Context, name string, _ ...tracer.StartOption) (context.Context, tracer.Span) {
	m.Name = name
	m.Span = &mockSpan{}
	return ctx, m.Span
}

func TestTrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name       string
		Method     string
		Path       string
		Status     int
		SpanStatus tracer.Status
		StatusDesc string
	}{
		{Name: "ok response", Method: http.MethodPost, Path: "/api/users?page=1", Status: http.StatusCreated},
		{Name: "redirect response", Method: http.MethodGet, Path: "/old", Status: http.StatusMovedPermanently},
		{Name: "client error response", Method: http.MethodGet, Path: "/missing", Status: http.StatusNotFound, SpanStatus: tracer.StatusError, StatusDesc: http.StatusText(http.StatusNotFound)},
		{Name: "server error response", Method: http.MethodGet, Path: "/broken", Status: http.StatusInternalServerError, SpanStatus: tracer.StatusError, StatusDesc: http.StatusText(http.StatusInternalServerError)},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			mockT := &mockTracer{}
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.Status)
			})
			req := httptest.NewRequest(tt.Method, tt.Path, nil)
			rec := testutil.NewTestResponseWriter()

			Trace(mockT)(handler).ServeHTTP(rec, req)

			require.Equal(t, tt.Status, rec.Code)
			require.Equal(t, tt.Method+" "+req.URL.Path, mockT.Name)
			require.True(t, mockT.Span.Ended)
			require.Equal(t, tt.SpanStatus, mockT.Span.Status)
			require.Equal(t, tt.StatusDesc, mockT.Span.StatusDesc)

			attrs := traceAttrs(mockT.Span)
			require.Equal(t, tt.Method, attrs["http.method"])
			require.Equal(t, req.URL.String(), attrs["http.url"])
			require.Equal(t, tt.Status, attrs["http.status_code"])
		})
	}
}

func TestTraceUsesExistingWriterProxy(t *testing.T) {
	t.Parallel()

	var captured http.ResponseWriter
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		captured = w
		w.WriteHeader(http.StatusOK)
	})
	mockT := &mockTracer{}
	rec := testutil.NewTestResponseWriter()
	ww, err := WrapWriter(rec)
	require.NoError(t, err)

	Trace(mockT)(handler).ServeHTTP(ww, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, ww, captured)
}

func traceAttrs(span *mockSpan) map[string]any {
	attrs := make(map[string]any, len(span.Attributes))
	for _, attr := range span.Attributes {
		attrs[attr.Key] = attr.Value
	}
	return attrs
}
