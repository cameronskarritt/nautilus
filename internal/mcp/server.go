package mcp

import (
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/log"
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
	srv.SetHandler(NewHandler(db, cfg.Logger))
	return srv
}
