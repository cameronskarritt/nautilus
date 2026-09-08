package documents

import (
	"net/http"

	"go.temporal.io/sdk/client"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/app/handlers/documents"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
)

func Mount(r *mux.Router, db database.Database, store objectstore.Store, workflows client.Client) {
	m := documents.NewMux(db, store, workflows)
	for path, handler := range map[string]http.HandlerFunc{
		"/documents":                             m.List,
		"/documents/{documentID:<uuid>}":         m.Get,
		"/documents/{documentID:<uuid>}/content": m.Content,
	} {
		r.Handle(http.MethodGet, path, authentication.RequireScopes(apikeys.ScopeRead)(version.Use(version.Versions{
			version.Version20260101: handler,
		})))
	}
}
