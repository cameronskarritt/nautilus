package notifications

import (
	"nautilus/internal/app/handlers/notifications/webpush"
	"nautilus/internal/database"
	"nautilus/internal/mux"
)

type NotificationsMux struct {
	webpush *webpush.WebPushMux
}

func NewMux(db database.Database, vapidPublicKey string) *NotificationsMux {
	return &NotificationsMux{
		webpush: webpush.NewMux(db, vapidPublicKey),
	}
}

func (m *NotificationsMux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	m.webpush.Mount(sub, "/webpush")
}
