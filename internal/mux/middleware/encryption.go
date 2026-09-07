package middleware

import (
	"context"
	"net/http"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/kms"
	"nautilus/internal/mux"
)

func UserEncryption(manager kms.KeyManager) mux.Middleware {
	enc := encrypt.ForUser(manager)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(encrypt.WithContext(r.Context(), enc)))
		})
	}
}

func OrganizationEncryption(manager kms.KeyManager) mux.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			var enc *encrypt.Encrypter
			org := organizations.FromContext(ctx)
			if org != nil && org.ID > 0 && org.ExternalID != "" && canEncryptOrganization(ctx, org.ID) {
				enc = encrypt.ForOrganization(manager, org.ExternalID)
			}
			// Mask any earlier shared-user capability, including when no organization is authorized.
			next.ServeHTTP(w, r.WithContext(encrypt.WithContext(ctx, enc)))
		})
	}
}

func canEncryptOrganization(ctx context.Context, orgID int) bool {
	if key := apikeys.FromContext(ctx); key != nil {
		return key.ID > 0 && key.OrganizationID == orgID
	}
	user := users.FromContext(ctx)
	member := organizations.MemberFromContext(ctx)
	return sessions.FromContext(ctx) > 0 && user != nil && user.ID > 0 &&
		member != nil && member.ID > 0 && member.UserID == user.ID && member.OrganizationID == orgID
}
