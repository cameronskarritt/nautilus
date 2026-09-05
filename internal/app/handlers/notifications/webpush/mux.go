package webpush

import (
	"nautilus/internal/database"
	"nautilus/internal/mux"
)

type WebPushMux struct {
	db             database.Database
	vapidPublicKey string
}

func NewMux(db database.Database, vapidPublicKey string) *WebPushMux {
	return &WebPushMux{
		db:             db,
		vapidPublicKey: vapidPublicKey,
	}
}

func (m *WebPushMux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	sub.Get("/vapid-key", m.GetVAPIDKey)
	sub.Post("/subscriptions", m.Subscribe)
	sub.Delete("/subscriptions", m.Unsubscribe)
}
