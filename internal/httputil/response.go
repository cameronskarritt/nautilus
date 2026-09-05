package httputil

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/observability/stacktrace"
)

type Map map[string]any

func DecodeJSON(r io.Reader, v any) error {
	err := json.NewDecoder(r).Decode(v)
	if err != nil {
		return errors.Wrap(err, "failed to decode JSON")
	}
	return nil
}

func JSON(ctx context.Context, w http.ResponseWriter, data any, code ...int) {
	status := http.StatusOK

	if len(code) > 0 {
		status = code[0]
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Error("error encoding JSON response", "error", err)
	}
}

func Error(ctx context.Context, w http.ResponseWriter, err error) {

	// Convert to HTTPError to standardize response
	httpErr := new(errors.HTTPError)
	isHTTPError := errors.As(err, &httpErr)
	if !isHTTPError {
		// Remain ambiguous for errors that aren't explicitly handled
		httpErr = errors.ErrInternalServerError
	}

	logger := log.FromContext(ctx)
	args := []any{"status", httpErr.Status}

	if httpErr.Status >= http.StatusInternalServerError {
		args = append(args, "error", err)
		if !errors.Is(err, errors.ErrInternalServerError) {
			stacktrace.Capture(ctx, err, nil)
		}
	}

	logger.With(args...).Error(httpErr.Message)

	JSON(ctx, w, httpErr, httpErr.Status)
}
