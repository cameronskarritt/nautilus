package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"slices"
	"time"
	"uuid"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
)

func organizationAccess(w http.ResponseWriter, r *http.Request, scope apikeys.Scope) (*organizations.Organization, error) {
	w.Header().Set("Cache-Control", "no-store")
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
	if sessions.FromContext(ctx) <= 0 || user == nil || user.ID <= 0 || member == nil || member.ID <= 0 || member.UserID != user.ID || member.OrganizationID != org.ID || !member.Role.CanManageOrg() {
		return nil, ErrForbidden
	}
	return org, nil
}

func (m *Mux) List(w http.ResponseWriter, r *http.Request) {
	org, err := organizationAccess(w, r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(r.Context(), w, err)
		return
	}
	params, err := listParams(r)
	if err != nil {
		httputil.Error(r.Context(), w, err)
		return
	}
	page, err := webhooks.List(r.Context(), m.db, org.ID, params)
	if err != nil {
		httputil.Error(r.Context(), w, publicError(err))
		return
	}
	httputil.JSON(r.Context(), w, page)
}
func (m *Mux) Get(w http.ResponseWriter, r *http.Request) {
	org, err := organizationAccess(w, r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(r.Context(), w, err)
		return
	}
	hook, err := m.webhook(r, org.ID)
	if err != nil {
		httputil.Error(r.Context(), w, err)
		return
	}
	httputil.JSON(r.Context(), w, httputil.Map{"webhook": hook})
}
func (m *Mux) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeWrite)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	var form CreateForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	id := uuid.NewV4().String()
	secret, token, err := newSecret(ctx, org.ExternalID, id)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	hook, err := webhooks.Create(ctx, m.db, org.ID, &webhooks.CreateOptions{ExternalID: id, Name: form.Name, URL: form.URL, EventTypes: form.EventTypes, SigningSecret: secret})
	if err != nil {
		httputil.Error(ctx, w, publicError(err))
		return
	}
	if hook == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"webhook": hook, "secret": token}, http.StatusCreated)
}
func (m *Mux) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeWrite)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	id, ok := mux.PathParam(r, "webhookID")
	if !ok {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	var form UpdateForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	opts := &webhooks.UpdateOptions{Name: form.Name, URL: form.URL, EventTypes: form.EventTypes}
	if form.Enabled.Set {
		opts.Enabled = optional.Set(*form.Enabled.Data)
	}
	hook, err := webhooks.Update(ctx, m.db, org.ID, id, opts)
	if err != nil {
		httputil.Error(ctx, w, publicError(err))
		return
	}
	if hook == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"webhook": hook})
}
func (m *Mux) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeWrite)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	id, ok := mux.PathParam(r, "webhookID")
	if !ok {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	deleted, err := webhooks.Delete(ctx, m.db, org.ID, id)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if !deleted {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (m *Mux) RotateSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeWrite)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	hook, err := m.webhook(r, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	secret, token, err := newSecret(ctx, org.ExternalID, hook.ExternalID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	hook, err = webhooks.RotateSecret(ctx, m.db, org.ID, hook.ExternalID, secret, time.Now().Add(24*time.Hour))
	if err != nil {
		httputil.Error(ctx, w, publicError(err))
		return
	}
	if hook == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"webhook": hook, "secret": token})
}
func newSecret(ctx context.Context, orgID, id string) ([]byte, string, error) {
	enc := encrypt.FromContext(ctx)
	if !enc.IsOrganization(orgID) {
		return nil, "", ErrForbidden
	}
	secret := make([]byte, 32)
	defer clear(secret)
	if _, err := rand.Read(secret); err != nil {
		return nil, "", errors.Wrap(err, "unable to generate webhook secret")
	}
	sealed, err := enc.Seal(ctx, secret, encrypt.Binding{Purpose: "webhook-signing-secret", RecordID: id})
	if err != nil {
		return nil, "", err
	}
	return sealed, "whsec_" + base64.StdEncoding.EncodeToString(secret), nil
}
func (m *Mux) webhook(r *http.Request, orgID int) (*webhooks.Webhook, error) {
	id, ok := mux.PathParam(r, "webhookID")
	if !ok {
		return nil, errors.ErrNotFound
	}
	hook, err := webhooks.GetByExternalID(r.Context(), m.db, orgID, id)
	if err != nil {
		return nil, err
	}
	if hook == nil {
		return nil, errors.ErrNotFound
	}
	return hook, nil
}
func listParams(r *http.Request) (pagination.Params, error) {
	params, err := pagination.ParseParams(r, 100)
	if err != nil || r.URL.Query().Get("cursor") != "" && params.Cursor == nil {
		return params, ErrCursor
	}
	if r.URL.Query().Get("limit") == "" {
		params.Limit = pagination.DefaultLimit
	}
	return params, nil
}
func publicError(err error) error {
	switch {
	case errors.Is(err, webhooks.ErrInvalidCursor):
		return ErrCursor
	case errors.Is(err, webhooks.ErrInvalidName):
		return FormError(ErrName)
	case errors.Is(err, webhooks.ErrInvalidEventTypes):
		return FormError(ErrEventTypes)
	case errors.Is(err, webhooks.ErrLimit):
		return ErrLimit
	case errors.Is(err, webhooks.ErrConflict):
		return ErrRotation
	default:
		return err
	}
}
