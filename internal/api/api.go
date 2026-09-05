package api

import (
	"nautilus/internal/api/authentication"
	"nautilus/internal/api/handlers"
	"nautilus/internal/api/handlers/apikeys"
	"nautilus/internal/api/version"
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/server"
)

type API struct {
	srv    *server.Server
	logger *log.Logger
}

type Config struct {
	Logger *log.Logger
}

func New(apiconfig *Config) *API {
	srv := server.New(&server.Config{
		Logger: apiconfig.Logger,
	})
	ctx := srv.Context()
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		apiconfig.Logger.Fatal("error connecting to database", "error", err)
	}
	srv.RegisterOnShutdown(database.Close(ctx, db))

	r := mux.New(mux.Config{
		Middleware: []mux.Middleware{
			middleware.AccessLog,
			middleware.Recover,
			authentication.RequireAPIKey(db),
			version.Middleware,
		},
		NotFoundHandler:         handlers.NotFoundHandler,
		MethodNotAllowedHandler: handlers.MethodNotAllowedHandler,
	})
	apikeys.Mount(r)

	srv.SetHandler(r)

	return &API{
		srv:    srv,
		logger: apiconfig.Logger,
	}
}

func (api *API) Serve() {
	api.srv.Serve()
}
