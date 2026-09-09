package main

import (
	"nautilus/internal/log"
	"nautilus/internal/mcpserver"
)

func main() {
	mcpserver.New(&mcpserver.Config{
		Logger: log.InferLogger("mcp"),
	}).Serve()
}
