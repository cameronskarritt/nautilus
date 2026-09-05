package handlers

import (
	"net/http"
	"slices"

	"nautilus/internal/httputil"
)

type EnvResponse struct {
	Version int     `json:"version"`
	Auth    EnvAuth `json:"auth"`
}

type EnvAuth struct {
	SSOProviders []string `json:"sso_providers"`
}

func Env(ssoProviders []string) http.HandlerFunc {
	ssoProviders = append([]string{}, ssoProviders...)
	slices.Sort(ssoProviders)

	env := EnvResponse{
		Version: 1,
		Auth: EnvAuth{
			SSOProviders: ssoProviders,
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		httputil.JSON(r.Context(), w, env)
	}
}
