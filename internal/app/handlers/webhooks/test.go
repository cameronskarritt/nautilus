package webhooks

import (
	"net/http"
	"uuid"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/enums"
	"nautilus/internal/httputil"
	"nautilus/internal/workflows/webhookdelivery"
)

func (m *Mux) Test(w http.ResponseWriter, r *http.Request) {
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
	if !hook.Enabled {
		httputil.Error(ctx, w, ErrDisabled)
		return
	}
	if m.workflows == nil {
		httputil.Error(ctx, w, ErrWorkflowUnavailable)
		return
	}
	id := uuid.NewV4().String()
	if err := webhookdelivery.StartTest(ctx, m.workflows, webhookdelivery.TestInput{OrganizationID: org.ID, WebhookID: hook.ExternalID, DeliveryID: id}); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"id": id, "status": enums.DeliveryStatusPending}, http.StatusAccepted)
}
