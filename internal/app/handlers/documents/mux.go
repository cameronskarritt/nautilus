package documents

import (
	"go.temporal.io/sdk/client"

	"nautilus/internal/database"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
)

type Mux struct {
	db        database.Database
	store     objectstore.Store
	workflows client.Client
}

func NewMux(db database.Database, store objectstore.Store, workflows client.Client) *Mux {
	return &Mux{db: db, store: store, workflows: workflows}
}

func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Get("/", m.List)
	sub.Post("/", m.Upload)
	sub.Get("/{documentID:<uuid>}", m.Get)
	sub.Get("/{documentID:<uuid>}/content", m.Content)
}
