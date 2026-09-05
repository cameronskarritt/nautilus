package db

import (
	"context"

	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/log"
)

func initialize(ctx context.Context) {
	logger := log.FromContext(ctx)

	logger.Info("connecting to database ...")
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		logger.Fatal("error connecting to database", "error", err)
	}
	logger.Info("database connected")

	logger.Info("initializing database ...")
	err = database.Initialize(ctx, db, postgres.Migrator{})
	if err != nil {
		logger.Fatal("error initializing database", "error", err)
	}

	logger.Info("database initialized")
}
