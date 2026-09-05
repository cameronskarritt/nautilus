package admin

import (
	"nautilus/internal/database"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
)

type AdminMux struct {
	db database.Database
}

func NewMux(db database.Database) *AdminMux {
	return &AdminMux{
		db: db,
	}
}

func (a *AdminMux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Use(middleware.RequireAdmin)

	sub.Get("/organizations", a.ListOrganizations)
	sub.Get("/organizations/{orgId:<uuid>}/flags", a.GetOrganizationFlags)
	sub.Patch("/organizations/{orgId:<uuid>}/flags", a.UpdateOrganizationFlag)

	sub.Get("/flags", a.ListFlags)
	sub.Post("/flags", a.CreateFlag)
	sub.Patch("/flags/{id:<int>}", a.UpdateFlag)
	sub.Delete("/flags/{id:<int>}", a.DeleteFlag)
}
