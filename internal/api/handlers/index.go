package handlers

import (
	"net/http"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
)

func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	httputil.Error(ctx, w, errors.ErrNotFound)
}

func MethodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	httputil.Error(ctx, w, errors.ErrMethodNotAllowed)
}
