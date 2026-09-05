package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"nautilus/internal/httputil"
)

type CORSConfig struct {
	AllowOrigins string

	// Preflight: List request headers and methods that can be used when making requests
	AllowHeaders string
	AllowMethods []string

	// Preflight: Indicate if the request can be made using credentials
	// Request: Indicate whether the response can be exposed when credentials flag is true
	AllowCredentials bool

	// Whitelist headers that clients may access
	ExposeHeaders string

	// Cached preflight requests TTL
	MaxAge int
}

func defaultConfig() *CORSConfig {
	return &CORSConfig{
		AllowOrigins: "*",
	}
}

func (config *CORSConfig) Origins() []string {
	trimmed := strings.ReplaceAll(config.AllowOrigins, " ", "")
	return strings.Split(trimmed, ",")
}

func CORS(config *CORSConfig) func(http.Handler) http.Handler {
	if config == nil {
		config = defaultConfig()
	}
	origins := config.Origins()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Only set Access-Control-Allow-Origin if origins are matched
			var allow string

			for _, pattern := range origins {
				// https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS#access-control-allow-origin

				// Accept any origin on wildcard, only return "*" for requests without credentials
				if pattern == "*" && config.AllowCredentials {
					allow = origin
					break
				}

				p, matches := httputil.MatchOrigin(origin, pattern)
				if matches {
					allow = p
					break
				}
			}

			// Simple requests
			// https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS#simple_requests

			// Preflight requests are sent using the OPTIONS, assume a request is simple otherwise
			if r.Method != http.MethodOptions {
				// If the server specifies a single origin rather than a wildcard, include a Vary
				// header to indicate to the client that responses will differ based on origin
				if config.AllowOrigins != "*" {
					w.Header().Set("Vary", "Origin")
				}

				// Tell the browser to allow this origin to access the resource
				if allow != "" {
					w.Header().Set("Access-Control-Allow-Origin", allow)
				}

				if config.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}

				if config.ExposeHeaders != "" {
					w.Header().Set("Access-Control-Expose-Headers", config.ExposeHeaders)
				}

				next.ServeHTTP(w, r)
				return
			}

			// Preflighted requests
			// https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS#preflighted_requests

			// Default headers to add to Vary for preflight requests
			preflightVary := []string{
				"Access-Control-Request-Headers",
				"Access-Control-Request-Method",
			}

			// Add Origin to Vary if not a wildcard
			if config.AllowOrigins != "*" {
				preflightVary = append(preflightVary, "Origin")
			}

			vary := strings.Join(preflightVary, ", ")
			w.Header().Set("Vary", vary)

			if config.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
			}

			// Set Allow-* headers
			if allow != "" {
				w.Header().Set("Access-Control-Allow-Origin", allow)
			}

			if len(config.AllowMethods) > 0 {
				methods := strings.Join(config.AllowMethods, ",")
				w.Header().Set("Access-Control-Allow-Methods", methods)
			}

			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Fall back to Request-Headers if Allow-Headers is empty
			if config.AllowHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", config.AllowHeaders)
			} else {
				fallback := r.Header.Get("Access-Control-Request-Headers")
				if fallback != "" {
					w.Header().Set("Access-Control-Allow-Headers", fallback)
				}
			}

			w.WriteHeader(http.StatusNoContent)
		})
	}
}
