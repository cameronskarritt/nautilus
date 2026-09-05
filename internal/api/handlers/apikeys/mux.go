package apikeys

import (
	"net/http"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/mux"
)

func Mount(r *mux.Router) {
	r.Handle(
		http.MethodGet,
		"/api-keys/current",
		authentication.RequireScopes(apikeys.ScopeRead)(version.Use(version.Versions{
			version.Version20260101: Current,
		})),
	)
}
