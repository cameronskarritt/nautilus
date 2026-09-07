package documents

import (
	"nautilus/internal/database"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
)

type Mux struct {
	db    database.Database
	store objectstore.Store
}

func NewMux(db database.Database, store objectstore.Store) *Mux { return &Mux{db: db, store: store} }

func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Get("/", m.List)
	sub.Post("/", m.Upload)
	sub.Get("/{documentID:<uuid>}", m.Get)
}
