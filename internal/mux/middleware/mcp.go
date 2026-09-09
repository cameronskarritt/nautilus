package middleware

import (
	"net/http"
	"strings"

	"nautilus/internal/api/authentication"
	"nautilus/internal/database"
	"nautilus/internal/database/oauth"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mux"
)

var ErrMCPAuth = errors.NewHTTPError(http.StatusUnauthorized, "Authentication required", errors.ErrorDetail{
	Message: "a valid MCP access token or API key is required",
	Code:    errors.ErrorCodeMCP01,
})

func MCPAuth(db database.Database, issuer string) mux.Middleware {
	issuer = strings.TrimRight(issuer, "/")
	return func(next http.Handler) http.Handler {
		keyAuth := authentication.RequireAPIKey(db)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, present := r.Header["X-Api-Key"]; present {
				keyAuth.ServeHTTP(w, r)
				return
			}

			var token string
			if headers := r.Header.Values("Authorization"); len(headers) == 1 {
				parts := strings.Fields(headers[0])
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					token = parts[1]
				}
			}
			if strings.HasPrefix(token, "nautilus_") {
				keyAuth.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			challenge := `Bearer resource_metadata="` + issuer + `/.well-known/oauth-protected-resource/mcp"`
			if r.Header.Get("Authorization") != "" {
				challenge += `, error="invalid_token"`
			}
			grant, err := oauth.Authenticate(ctx, db, token, issuer+"/mcp")
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if grant == nil {
				w.Header().Set("WWW-Authenticate", challenge)
				httputil.Error(ctx, w, ErrMCPAuth)
				return
			}
			user, err := users.Get(ctx, db, grant.UserID)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			member, err := organizations.GetMember(ctx, db, grant.MemberID)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			org, err := organizations.Get(ctx, db, grant.OrganizationID)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if user == nil || member == nil || org == nil || member.UserID != user.ID || member.OrganizationID != org.ID {
				w.Header().Set("WWW-Authenticate", challenge)
				httputil.Error(ctx, w, ErrMCPAuth)
				return
			}
			ctx = oauth.WithContext(ctx, grant)
			ctx = users.WithContext(ctx, user)
			ctx = organizations.WithMemberContext(ctx, member)
			ctx = organizations.WithContext(ctx, org)
			ctx = log.WithContext(ctx, log.FromContext(ctx).With(
				"oauth_grant_id", grant.ID, "user_id", user.ID, "organization_id", org.ID,
			))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
