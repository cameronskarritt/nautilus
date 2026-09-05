package main

import (
	"nautilus/internal/api"
	"nautilus/internal/log"
)

func main() {
	config := &api.Config{
		Logger: log.InferLogger("api"),
	}

	api.New(config).Serve()
}
