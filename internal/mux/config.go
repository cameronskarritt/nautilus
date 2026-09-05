package mux

import "net/http"

type Config struct {
	Middleware              []Middleware
	NotFoundHandler         http.HandlerFunc
	MethodNotAllowedHandler http.HandlerFunc
}

func (r *Router) UseConfig(config Config) {
	r.Use(config.Middleware...)

	if config.NotFoundHandler != nil {
		r.notFoundHandler = config.NotFoundHandler
	}

	if config.MethodNotAllowedHandler != nil {
		r.methodNotAllowedHandler = config.MethodNotAllowedHandler
	}
}
