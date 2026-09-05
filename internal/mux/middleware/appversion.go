package middleware

import "net/http"

// AppVersion adds the X-App-Version header to all responses.
// If version is empty, no header is set.
func AppVersion(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if version != "" {
				w.Header().Set("X-App-Version", version)
			}
			next.ServeHTTP(w, r)
		})
	}
}
