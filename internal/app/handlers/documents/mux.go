package documents

import (
	"nautilus/internal/database"
	"nautilus/internal/mux"
)

type Mux struct {
	db database.Database
}

func NewMux(db database.Database) *Mux { return &Mux{db: db} }

func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Get("/", m.List)
	sub.Get("/{documentID:<uuid>}", m.Get)
}
