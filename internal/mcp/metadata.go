package mcp

import (
	"net/http"

	"nautilus/internal/enums"
	"nautilus/internal/httputil"
	"nautilus/internal/oauth"
)

func resourceMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.JSON(r.Context(), w, httputil.Map{
		"resource":                 oauth.Resource(),
		"authorization_servers":    []string{oauth.Issuer()},
		"scopes_supported":         []enums.Scope{enums.ScopeRead, enums.ScopeWrite},
		"bearer_methods_supported": []string{"header"},
	})
}
