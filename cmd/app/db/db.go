package db

import (
	"context"

	"nautilus/internal/log"
)

func Run(args []string) {
	ctx := context.Background()

	logger := log.InferLogger("db")
	ctx = log.WithContext(ctx, logger)

	cmd := args[0]

	switch cmd {
	case "init", "initialize":
		initialize(ctx)
	case "migrate":
		migrate(ctx)
	default:
		logger.Fatal("unrecognized command", "cmd", cmd)
	}

}
