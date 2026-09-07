package main

import (
	"os"

	"nautilus/internal/log"

	"nautilus/cmd/app/db"
	"nautilus/cmd/app/keys"
	"nautilus/cmd/app/serve"
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
	default:
		logger.Fatal("unrecognized command", "command", cmd)
	}
}
