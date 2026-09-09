package apikeys

import (
	"net/http"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
)

func Mount(r *mux.Router) {
	r.Handle(
		http.MethodGet,
		"/api-keys/current",
		authentication.RequireScopes(enums.ScopeRead)(version.Use(version.Versions{
			version.Version20260101: Current,
		})),
	)
}
