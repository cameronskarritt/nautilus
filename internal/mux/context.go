package mux

import (
	"context"
	"net/http"
)

type contextKey struct{}

func setPathParams(r *http.Request, params map[string]string) *http.Request {
	ctx := context.WithValue(r.Context(), contextKey{}, params)
	return r.WithContext(ctx)
}

func PathParam(r *http.Request, key string) (string, bool) {
	ctx := r.Context()
	params, ok := ctx.Value(contextKey{}).(map[string]string)
	if !ok {
		return "", false
	}

	value, exists := params[key]
	return value, exists
}
