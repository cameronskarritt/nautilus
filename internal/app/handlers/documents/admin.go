package documents

import (
	"context"
	"net/http"
	"uuid"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/auditlogs"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/httputil"
	"nautilus/internal/kms"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/optional"
)

// MountAdmin provides explicit privileged intake without granting organization membership.
func (m *Mux) MountAdmin(r *mux.Router, prefix string, keys kms.KeyManager) {
	admin := *m
	admin.admin = true
	sub := r.SubRouter(prefix)
	sub.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			ctx := r.Context()
			if !sessionAdmin(ctx) {
				httputil.Error(ctx, w, middleware.ErrAdminRequired)
				return
			}
			id, ok := mux.PathParam(r, "orgID")
			if !ok {
				httputil.Error(ctx, w, middleware.ErrOrgNotFound)
				return
			}
			org, err := organizations.GetByExternalID(ctx, m.db, id)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if org == nil {
				httputil.Error(ctx, w, middleware.ErrOrgNotFound)
				return
			}
			ctx = organizations.WithContext(ctx, org)
			ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(keys, org.ExternalID))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	sub.Post("/", admin.Upload)
	sub.Get("/{documentID:<uuid>}", admin.Get)
	sub.Get("/{documentID:<uuid>}/content", admin.Content)
}

func sessionAdmin(ctx context.Context) bool {
	user := users.FromContext(ctx)
	return apikeys.FromContext(ctx) == nil && sessions.FromContext(ctx) > 0 && user != nil && user.ID > 0 && user.Admin
}

func (m *Mux) organizationAccess(r *http.Request, scope apikeys.Scope) (*organizations.Organization, error) {
	if !m.admin {
		return organizationAccess(r, scope)
	}
	org := organizations.FromContext(r.Context())
	id, ok := mux.PathParam(r, "orgID")
	parsed, err := uuid.Parse(id)
	if err != nil || !sessionAdmin(r.Context()) || !ok || org == nil || org.ID <= 0 || org.ExternalID != parsed.String() {
		return nil, ErrForbidden
	}
	return org, nil
}

func (m *Mux) auditAccess(ctx context.Context, orgID int, documentID string, kind enums.AuditType) error {
	if !m.admin {
		return nil
	}
	_, err := auditlogs.Create(ctx, m.db, users.FromContext(ctx).ID, kind, optional.Set(orgID), optional.Set[any](httputil.Map{"document_id": documentID}))
	return err
}
