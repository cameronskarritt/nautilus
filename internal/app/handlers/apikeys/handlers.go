package apikeys

import (
	"net/http"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
)

func (m *Mux) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	organization, _, err := organizationAccess(r)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	keys, err := apikeys.List(ctx, m.db, organization.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(ctx, w, httputil.Map{"api_keys": keys})
}

func (m *Mux) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	organization, user, err := organizationAccess(r)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	var form CreateForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	key, token, err := apikeys.Create(ctx, m.db, organization.ID, user.ID, form.Options())
	if database.IsUniqueViolationOn(err, "idx_api_keys_organization_name_active") {
		httputil.Error(ctx, w, ErrNameExists)
		return
	}
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(ctx, w, httputil.Map{
		"api_key": key,
		"token":   token,
	}, http.StatusCreated)
}

func (m *Mux) Revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	organization, _, err := organizationAccess(r)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	apiKeyID, _ := mux.PathParam(r, "apiKeyId")
	revoked, err := apikeys.RevokeByExternalID(ctx, m.db, organization.ID, apiKeyID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if !revoked {
		httputil.Error(ctx, w, ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func organizationAccess(
	r *http.Request,
) (*organizations.Organization, *users.User, error) {
	ctx := r.Context()
	organization := organizations.FromContext(ctx)
	member := organizations.MemberFromContext(ctx)
	user := users.FromContext(ctx)
	if organization == nil || member == nil || user == nil ||
		member.OrganizationID != organization.ID || member.UserID != user.ID {
		return nil, nil, ErrOrganizationRequired
	}
	if !member.Role.CanManageOrg() {
		return nil, nil, ErrForbidden
	}
	return organization, user, nil
}
