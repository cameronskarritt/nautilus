package mcp

import (
	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/server"
)

type Config struct {
	Logger *log.Logger
}

func New(cfg *Config) *server.Server {
	srv := server.New(&server.Config{
		Addr:   config.Get("MCP_ADDRESS", ":8082"),
		Logger: cfg.Logger,
	})
	ctx := srv.Context()
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		cfg.Logger.Fatal("error connecting to database", "error", err)
	}
	srv.RegisterOnShutdown(database.Close(ctx, db))
	awsCfg, err := aws.LoadConfig(ctx)
	if err != nil {
		cfg.Logger.Fatal("error loading AWS config", "error", err)
	}
	var store objectstore.Store
	if bucket := config.Get[string]("DOCUMENTS_BUCKET"); bucket != "" {
		store = s3store.New(awsCfg, bucket, awsCfg.BaseEndpoint != nil)
	}
	srv.SetHandler(NewHandler(db, store, awskms.New(awsCfg, db), cfg.Logger))
	return srv
}
