package middleware

import (
	"net/http"

	"nautilus/internal/observability/stacktrace"
)

func StackTrace(tracer stacktrace.StackTracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tracer == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := stacktrace.WithContext(r.Context(), tracer)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
