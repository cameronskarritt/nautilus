package webhooks

import (
	"go.temporal.io/sdk/client"

	"nautilus/internal/database"
	"nautilus/internal/mux"
)

type Mux struct {
	db        database.Database
	workflows client.Client
}

func NewMux(db database.Database, workflows client.Client) *Mux {
	return &Mux{db: db, workflows: workflows}
}
func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Get("/", m.List)
	sub.Post("/", m.Create)
	sub.Get("/{webhookID:<uuid>}", m.Get)
	sub.Patch("/{webhookID:<uuid>}", m.Update)
	sub.Delete("/{webhookID:<uuid>}", m.Delete)
	sub.Post("/{webhookID:<uuid>}/secret/rotate", m.RotateSecret)
	sub.Post("/{webhookID:<uuid>}/test", m.Test)
	sub.Get("/{webhookID:<uuid>}/deliveries", m.ListDeliveries)
	sub.Get("/{webhookID:<uuid>}/deliveries/{deliveryID:<uuid>}", m.GetDelivery)
	sub.Get("/{webhookID:<uuid>}/deliveries/{deliveryID:<uuid>}/attempts", m.ListAttempts)
}
