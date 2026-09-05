package middleware

import (
	"net/http"

	"nautilus/internal/observability/tracer"
)

func Trace(t tracer.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := t.Start(r.Context(), r.Method+" "+r.URL.Path)
			defer span.End()

			span.SetAttributes(
				tracer.StringAttr("http.method", r.Method),
				tracer.StringAttr("http.url", r.URL.String()),
			)

			ww, ok := w.(WriterProxy)
			if !ok {
				var err error
				ww, err = WrapWriter(w)
				if err != nil {
					panic(err)
				}
			}

			next.ServeHTTP(ww, r.WithContext(ctx))

			span.SetAttributes(tracer.IntAttr("http.status_code", ww.Status()))
			if ww.Status() >= 400 {
				span.SetStatus(tracer.StatusError, http.StatusText(ww.Status()))
			}
		})
	}
}
