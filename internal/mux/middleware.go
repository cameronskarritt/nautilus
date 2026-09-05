package mux

import (
	"net/http"
)

type Middleware func(http.Handler) http.Handler

func (router *Router) applyMiddleware(handler http.Handler) http.Handler {
	for i := len(router.middleware) - 1; i >= 0; i-- {
		handler = router.middleware[i](handler)
	}

	return handler
}

func (router *Router) Use(mws ...Middleware) {
	router.middleware = append(router.middleware, mws...)
}
