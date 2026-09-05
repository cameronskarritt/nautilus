package version

import (
	"net/http"

	"nautilus/internal/httputil"
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		version := Version(r.Header.Get("X-API-Version"))
		if version == "" {
			version = VersionLatest
		}

		if !version.Validate() {
			httputil.Error(ctx, w, Error(ErrInvalidVersion))
			return
		}

		ctx = WithContext(ctx, version)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}
