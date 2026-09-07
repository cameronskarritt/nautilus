package documents

import (
	"net/http"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/app/handlers/documents"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/mux"
)

func Mount(r *mux.Router, db database.Database) {
	m := documents.NewMux(db)
	for path, handler := range map[string]http.HandlerFunc{
		"/documents":                     m.List,
		"/documents/{documentID:<uuid>}": m.Get,
	} {
		r.Handle(http.MethodGet, path, authentication.RequireScopes(apikeys.ScopeRead)(version.Use(version.Versions{
			version.Version20260101: handler,
		})))
	}
}
