package apikeys

import (
	"net/http"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
)

func Current(w http.ResponseWriter, r *http.Request) {
	key := apikeys.FromContext(r.Context())
	if key == nil {
		httputil.Error(r.Context(), w, errors.New("API key not found in request context"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(r.Context(), w, httputil.Map{"api_key": key})
}
