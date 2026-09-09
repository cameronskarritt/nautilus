package api

import (
	"nautilus/internal/api/authentication"
	"nautilus/internal/api/handlers"
	"nautilus/internal/api/handlers/apikeys"
	"nautilus/internal/api/handlers/documents"
	"nautilus/internal/api/handlers/webhooks"
	"nautilus/internal/api/version"
	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/objectstore"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/server"
	"nautilus/internal/temporal"
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
	awsCfg, err := aws.LoadConfig(ctx)
	if err != nil {
		apiconfig.Logger.Fatal("error loading AWS config", "error", err)
	}
	var documentStore objectstore.Store
	if bucket := config.Get[string]("DOCUMENTS_BUCKET"); bucket != "" {
		documentStore = s3store.New(awsCfg, bucket, awsCfg.BaseEndpoint != nil)
	}
	workflowClient, err := temporal.Dial(ctx)
	if err != nil {
		apiconfig.Logger.Fatal("error connecting to Temporal", "error", err)
	}
	srv.RegisterOnShutdown(workflowClient.Close)

	keys := awskms.New(awsCfg, db)

	r := mux.New(mux.Config{
		Middleware: []mux.Middleware{
			middleware.AccessLog,
			middleware.Recover,
			authentication.RequireAPIKey(db),
			middleware.OrganizationEncryption(keys),
			version.Middleware,
		},
		NotFoundHandler:         handlers.NotFoundHandler,
		MethodNotAllowedHandler: handlers.MethodNotAllowedHandler,
	})
	apikeys.Mount(r)
	documents.Mount(r, db, documentStore, workflowClient)
	webhooks.Mount(r, db, workflowClient)

	srv.SetHandler(r)

	return &API{
		srv:    srv,
		logger: apiconfig.Logger,
	}
}

func (api *API) Serve() {
	api.srv.Serve()
}
