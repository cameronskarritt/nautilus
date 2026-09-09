package webhooks

import (
	"net/http"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
)

func (m *Mux) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	hook, err := m.webhook(r, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	params, err := listParams(r)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	page, err := webhooks.ListDeliveries(ctx, m.db, org.ID, hook.ExternalID, params)
	if err != nil {
		httputil.Error(ctx, w, publicError(err))
		return
	}
	httputil.JSON(ctx, w, page)
}
func (m *Mux) GetDelivery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	delivery, err := m.delivery(r, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	event, err := webhookevents.GetByExternalID(ctx, m.db, org.ID, delivery.EventExternalID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if event == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"delivery": delivery, "payload": event.Payload})
}
func (m *Mux) ListAttempts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, err := organizationAccess(w, r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	delivery, err := m.delivery(r, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	params, err := listParams(r)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	page, err := webhooks.ListAttempts(ctx, m.db, org.ID, delivery.WebhookExternalID, delivery.ExternalID, params)
	if err != nil {
		httputil.Error(ctx, w, publicError(err))
		return
	}
	httputil.JSON(ctx, w, page)
}
func (m *Mux) delivery(r *http.Request, orgID int) (*webhooks.Delivery, error) {
	hook, err := m.webhook(r, orgID)
	if err != nil {
		return nil, err
	}
	id, ok := mux.PathParam(r, "deliveryID")
	if !ok {
		return nil, errors.ErrNotFound
	}
	delivery, err := webhooks.GetDelivery(r.Context(), m.db, orgID, id)
	if err != nil {
		return nil, err
	}
	if delivery == nil || delivery.WebhookID != hook.ID {
		return nil, errors.ErrNotFound
	}
	return delivery, nil
}
