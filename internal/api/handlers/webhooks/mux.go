package webhooks

import (
	"net/http"

	"go.temporal.io/sdk/client"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/app/handlers/webhooks"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/mux"
)

func Mount(r *mux.Router, db database.Database, workflows client.Client) {
	m := webhooks.NewMux(db, workflows)
	for _, route := range []struct {
		method  string
		path    string
		scope   apikeys.Scope
		handler http.HandlerFunc
	}{
		{http.MethodGet, "/webhooks", apikeys.ScopeRead, m.List},
		{http.MethodPost, "/webhooks", apikeys.ScopeWrite, m.Create},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}", apikeys.ScopeRead, m.Get},
		{http.MethodPatch, "/webhooks/{webhookID:<uuid>}", apikeys.ScopeWrite, m.Update},
		{http.MethodDelete, "/webhooks/{webhookID:<uuid>}", apikeys.ScopeWrite, m.Delete},
		{http.MethodPost, "/webhooks/{webhookID:<uuid>}/secret/rotate", apikeys.ScopeWrite, m.RotateSecret},
		{http.MethodPost, "/webhooks/{webhookID:<uuid>}/test", apikeys.ScopeWrite, m.Test},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}/deliveries", apikeys.ScopeRead, m.ListDeliveries},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}/deliveries/{deliveryID:<uuid>}", apikeys.ScopeRead, m.GetDelivery},
		{http.MethodGet, "/webhooks/{webhookID:<uuid>}/deliveries/{deliveryID:<uuid>}/attempts", apikeys.ScopeRead, m.ListAttempts},
	} {
		r.Handle(route.method, route.path, authentication.RequireScopes(route.scope)(version.Use(version.Versions{version.Version20260101: route.handler})))
	}
}
