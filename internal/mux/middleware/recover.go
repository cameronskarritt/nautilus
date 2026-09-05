package middleware

import (
	"net/http"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/observability/stacktrace"
)

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rc := recover(); rc != nil {
				var err error
				switch v := rc.(type) {
				case error:
					err = v
				case string:
					err = errors.New(v)
				default:
					err = errors.Errorf("panic: %v", v)
				}
				err = errors.WithStack(err)

				ctx := r.Context()
				logger := log.FromContext(ctx)
				logger.With("error", err).Error("recovered from panic")

				stacktrace.Capture(ctx, err, &stacktrace.CaptureOptions{Request: r})

				httputil.Error(ctx, w, errors.ErrInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
