package documents

import (
	"net/http"
	"slices"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
	"nautilus/internal/pagination"
)

func (m *Mux) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	org, err := m.organizationAccess(r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	params, err := pagination.ParseParams(r, 100)
	if err != nil || (r.URL.Query().Get("cursor") != "" && params.Cursor == nil) {
		httputil.Error(ctx, w, ErrInvalidCursor)
		return
	}
	if r.URL.Query().Get("limit") == "" {
		params.Limit = pagination.DefaultLimit
	}
	page, err := documents.List(ctx, m.db, org.ID, params)
	if errors.Is(err, documents.ErrInvalidCursor) {
		httputil.Error(ctx, w, ErrInvalidCursor)
		return
	}
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	httputil.JSON(ctx, w, page)
}

func (m *Mux) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	org, err := m.organizationAccess(r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	id, ok := mux.PathParam(r, "documentID")
	if !ok {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	doc, err := documents.GetByExternalID(ctx, m.db, org.ID, id)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if doc == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"document": doc})
}

func organizationAccess(r *http.Request, scope apikeys.Scope) (*organizations.Organization, error) {
	ctx := r.Context()
	org := organizations.FromContext(ctx)
	if org == nil || org.ID <= 0 || org.ExternalID == "" {
		return nil, ErrOrganizationRequired
	}
	if key := apikeys.FromContext(ctx); key != nil {
		if key.ID <= 0 || key.OrganizationID != org.ID || !slices.Contains(key.Scopes, scope) {
			return nil, ErrForbidden
		}
		return org, nil
	}
	user := users.FromContext(ctx)
	member := organizations.MemberFromContext(ctx)
	if sessions.FromContext(ctx) <= 0 || user == nil || user.ID <= 0 || member == nil || member.ID <= 0 ||
		member.UserID != user.ID || member.OrganizationID != org.ID || !member.Role.IsValid() {
		return nil, ErrForbidden
	}
	if scope == apikeys.ScopeWrite && member.Role == organizations.RoleViewer {
		return nil, ErrForbidden
	}
	return org, nil
}
