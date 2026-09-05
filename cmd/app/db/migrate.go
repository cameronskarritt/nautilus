package db

import (
	"context"

	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/log"
)

func migrate(ctx context.Context) {
	logger := log.FromContext(ctx)

	logger.Info("connecting to database ...")
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		logger.Fatal("error connecting to database", "error", err)
	}
	logger.Info("database connected")

	logger.Info("migrating database ...")
	var migrator postgres.Migrator
	err = database.Migrate(ctx, db, migrator)
	if err != nil {
		logger.Fatal("error migrating database", "error", err)
	}

	logger.Info("database migrated")
}
