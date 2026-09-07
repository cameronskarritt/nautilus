package documents

import (
	"net/http"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/app/handlers/documents"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
)

func Mount(r *mux.Router, db database.Database, store objectstore.Store) {
	m := documents.NewMux(db, store)
	for path, handler := range map[string]http.HandlerFunc{
		"/documents":                     m.List,
		"/documents/{documentID:<uuid>}": m.Get,
	} {
		r.Handle(http.MethodGet, path, authentication.RequireScopes(apikeys.ScopeRead)(version.Use(version.Versions{
			version.Version20260101: handler,
		})))
	}
	r.Handle(http.MethodPost, "/documents", authentication.RequireScopes(apikeys.ScopeWrite)(version.Use(version.Versions{
		version.Version20260101: m.Upload,
	})))
}
