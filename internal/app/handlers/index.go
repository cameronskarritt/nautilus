package handlers

import (
	"context"
	"net/http"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
)

func Panic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n := 0
	x := 1 / n
	panicData := httputil.Map{
		"message": "PANIC",
		"o":       x,
	}
	httputil.JSON(ctx, w, panicData, http.StatusOK)
}

func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	httputil.Error(ctx, w, errors.ErrNotFound)
}

func MethodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	httputil.Error(ctx, w, errors.ErrMethodNotAllowed)
}

func Healthcheck(serverCtx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		health := httputil.Map{
			"status": "ok",
		}

		if errors.Is(serverCtx.Err(), context.Canceled) {
			health["status"] = "shutting_down"
			httputil.JSON(ctx, w, health, http.StatusServiceUnavailable)
			return
		}

		httputil.JSON(ctx, w, health, http.StatusOK)
	}
}
