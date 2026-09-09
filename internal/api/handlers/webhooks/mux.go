package webhooks

import (
	"net/http"

	"go.temporal.io/sdk/client"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/app/handlers/webhooks"
	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
)

func Mount(r *mux.Router, db database.Database, workflows client.Client) {
	m := webhooks.NewMux(db, workflows)
	for _, route := range []struct {
		method  string
		path    string
		scope   enums.Scope
		handler http.HandlerFunc
	}{
		{http.MethodGet, "/webhooks", enums.ScopeRead, m.List},
		{http.MethodPost, "/webhooks", enums.ScopeWrite, m.Create},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}", enums.ScopeRead, m.Get},
		{http.MethodPatch, "/webhooks/{webhookID:<uuid>}", enums.ScopeWrite, m.Update},
		{http.MethodDelete, "/webhooks/{webhookID:<uuid>}", enums.ScopeWrite, m.Delete},
		{http.MethodPost, "/webhooks/{webhookID:<uuid>}/secret/rotate", enums.ScopeWrite, m.RotateSecret},
		{http.MethodPost, "/webhooks/{webhookID:<uuid>}/test", enums.ScopeWrite, m.Test},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}/deliveries", enums.ScopeRead, m.ListDeliveries},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}/deliveries/{deliveryID:<uuid>}", enums.ScopeRead, m.GetDelivery},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}/deliveries/{deliveryID:<uuid>}/attempts", enums.ScopeRead, m.ListAttempts},
	} {
		r.Handle(route.method, route.path, authentication.RequireScopes(route.scope)(version.Use(version.Versions{version.Version20260101: route.handler})))
	}
}
