package httputil

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/observability/stacktrace"
	"nautilus/internal/testutil/require"
)

type captureHandler struct {
	attrs []slog.Attr
	logs  *[]capturedLog
}

type capturedLog struct {
	Message string
	Attrs   []slog.Attr
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h captureHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := slices.Clone(h.attrs)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	*h.logs = append(*h.logs, capturedLog{
		Message: record.Message,
		Attrs:   attrs,
	})
	return nil
}

func (h captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := captureHandler{
		attrs: slices.Clone(h.attrs),
		logs:  h.logs,
	}
	clone.attrs = append(clone.attrs, attrs...)
	return clone
}

func (h captureHandler) WithGroup(string) slog.Handler {
	return h
}

func TestJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name   string
		Data   any
		Code   []int
		Status int
		Body   string
	}{
		{Name: "default status", Data: Map{"message": "hello"}, Status: http.StatusOK, Body: `{"message":"hello"}`},
		{Name: "custom status", Data: Map{"error": "not found"}, Code: []int{http.StatusNotFound}, Status: http.StatusNotFound, Body: `{"error":"not found"}`},
		{Name: "struct", Data: struct{ Name string }{"test"}, Status: http.StatusOK, Body: `{"Name":"test"}`},
		{Name: "slice", Data: []string{"a", "b"}, Code: []int{http.StatusCreated}, Status: http.StatusCreated, Body: `["a","b"]`},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			JSON(context.Background(), w, tt.Data, tt.Code...)

			result := w.Result()
			defer result.Body.Close()
			require.Equal(t, tt.Status, result.StatusCode)
			require.Equal(t, "application/json", result.Header.Get("Content-Type"))
			require.Equal(t, tt.Body+"\n", w.Body.String())
		})
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name    string
		Err     error
		Status  int
		Message string
	}{
		{
			Name:    "http error",
			Err:     errors.NewHTTPError(http.StatusBadRequest, "bad request", errors.ErrorDetail{Message: "invalid input"}),
			Status:  http.StatusBadRequest,
			Message: "bad request",
		},
		{
			Name:    "not found",
			Err:     errors.ErrNotFound,
			Status:  http.StatusNotFound,
			Message: "Unable to handle request",
		},
		{
			Name:    "internal fallback",
			Err:     errors.New("some internal error"),
			Status:  http.StatusInternalServerError,
			Message: "Unable to handle request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			Error(context.Background(), w, tt.Err)

			result := w.Result()
			defer result.Body.Close()
			require.Equal(t, tt.Status, result.StatusCode)
			require.Equal(t, "application/json", result.Header.Get("Content-Type"))

			var body struct {
				Message string `json:"message"`
			}
			err := json.NewDecoder(w.Body).Decode(&body)
			require.NoError(t, err)
			require.Equal(t, tt.Message, body.Message)
		})
	}
}

func TestErrorLogsStackForInternalServerError(t *testing.T) {
	t.Parallel()

	var logs []capturedLog
	logger := log.New(captureHandler{logs: &logs})
	ctx := log.WithContext(context.Background(), logger)
	rec := httptest.NewRecorder()

	Error(ctx, rec, errors.New("database exploded"))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Len(t, logs, 1)
	require.Equal(t, "Unable to handle request", logs[0].Message)

	var loggedErr error
	for _, attr := range logs[0].Attrs {
		if attr.Key == "error" {
			var ok bool
			loggedErr, ok = attr.Value.Any().(error)
			require.True(t, ok)
		}
	}

	require.NotNil(t, loggedErr)
	require.Contains(t, loggedErr.Error(), "database exploded")

	var st errors.StackTracer
	require.True(t, errors.As(loggedErr, &st))
	require.NotEmpty(t, st.StackTrace())
	require.NotContains(t, rec.Body.String(), "database exploded")
}

func TestErrorDoesNotLogOriginalErrorForClientError(t *testing.T) {
	t.Parallel()

	var logs []capturedLog
	logger := log.New(captureHandler{logs: &logs})
	ctx := log.WithContext(context.Background(), logger)
	rec := httptest.NewRecorder()

	Error(ctx, rec, errors.NewHTTPError(http.StatusBadRequest, "bad request"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Len(t, logs, 1)

	for _, attr := range logs[0].Attrs {
		require.NotEqual(t, "error", attr.Key)
		require.NotEqual(t, "errors", attr.Key)
	}
}

func TestErrorCapturesInternalServerError(t *testing.T) {
	t.Parallel()

	tracer := &recordingStackTracer{}
	ctx := stacktrace.WithContext(context.Background(), tracer)
	rec := httptest.NewRecorder()
	err := errors.New("database exploded")

	Error(ctx, rec, err)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, err, tracer.err)
	require.Nil(t, tracer.opts)
}

func TestErrorDoesNotCaptureClientError(t *testing.T) {
	t.Parallel()

	tracer := &recordingStackTracer{}
	ctx := stacktrace.WithContext(context.Background(), tracer)
	rec := httptest.NewRecorder()

	Error(ctx, rec, errors.NewHTTPError(http.StatusBadRequest, "bad request"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, tracer.err)
}

func TestErrorDoesNotCaptureGenericInternalServerError(t *testing.T) {
	t.Parallel()

	tracer := &recordingStackTracer{}
	ctx := stacktrace.WithContext(context.Background(), tracer)
	rec := httptest.NewRecorder()

	Error(ctx, rec, errors.ErrInternalServerError)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Nil(t, tracer.err)
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	type target struct {
		Key string `json:"key"`
	}

	tests := []struct {
		Name        string
		JSON        string
		ExpectedKey string
		ExpectedErr bool
	}{
		{Name: "valid json", JSON: `{"key":"value"}`, ExpectedKey: "value"},
		{Name: "malformed json", JSON: `{key: "value"}`, ExpectedErr: true},
		{Name: "empty json", ExpectedErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			var result target
			err := DecodeJSON(strings.NewReader(tt.JSON), &result)
			if tt.ExpectedErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "failed to decode JSON")
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.ExpectedKey, result.Key)
		})
	}
}

type recordingStackTracer struct {
	err  error
	opts *stacktrace.CaptureOptions
}

func (t *recordingStackTracer) Capture(_ context.Context, err error, opts *stacktrace.CaptureOptions) {
	t.err = err
	t.opts = opts
}
