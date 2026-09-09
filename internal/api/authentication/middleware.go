package authentication

import (
	"context"
	"net/http"
	"strings"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mux"
)

func RequireAPIKey(db database.Database) mux.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			token := apiKeyToken(r.Header)
			if token == "" {
				unauthorized(ctx, w)
				return
			}
			key, err := apikeys.Authenticate(ctx, db, token)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if key == nil {
				unauthorized(ctx, w)
				return
			}
			org, err := organizations.Get(ctx, db, key.OrganizationID)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if org == nil {
				unauthorized(ctx, w)
				return
			}

			logger := log.FromContext(ctx).With(
				"api_key_id", key.ID,
				"organization_id", key.OrganizationID,
			)
			ctx = log.WithContext(ctx, logger)
			ctx = apikeys.WithContext(ctx, key)
			ctx = organizations.WithContext(ctx, org)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireScopes(scopes ...enums.Scope) mux.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := apikeys.FromContext(r.Context())
			if key == nil {
				unauthorized(r.Context(), w)
				return
			}
			for _, scope := range scopes {
				if !hasScope(key, scope) {
					w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
					httputil.Error(r.Context(), w, ErrInsufficientScope)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func apiKeyToken(header http.Header) string {
	if keys, present := header["X-Api-Key"]; present {
		if len(keys) != 1 {
			return ""
		}
		return strings.TrimSpace(keys[0])
	}
	values := header.Values("Authorization")
	if len(values) != 1 {
		return ""
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func hasScope(key *apikeys.Key, required enums.Scope) bool {
	for _, scope := range key.Scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func unauthorized(ctx context.Context, w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	httputil.Error(ctx, w, ErrUnauthorized)
}
