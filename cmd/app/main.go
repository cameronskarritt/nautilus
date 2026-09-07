package main

import (
	"os"

	"nautilus/internal/log"

	"nautilus/cmd/app/db"
	"nautilus/cmd/app/keys"
	"nautilus/cmd/app/serve"
	"nautilus/cmd/app/temporal"
)

func main() {

	logger := log.InferLogger("main")

	cmd := os.Args[1]

	var args []string
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	switch cmd {
	case "serve":
		serve.Run()
	case "db":
		db.Run(args)
	case "keys":
		keys.Run(args)
	case "worker":
		if err := temporal.Worker(); err != nil {
			logger.Fatal("Temporal worker failed", "error", err)
		}
	case "temporal-smoke":
		if err := temporal.Smoke(); err != nil {
			logger.Fatal("Temporal smoke failed", "error", err)
		}
	default:
		logger.Fatal("unrecognized command", "command", cmd)
	}
}
