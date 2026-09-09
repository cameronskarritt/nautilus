package main

import (
	"nautilus/internal/log"
	"nautilus/internal/mcp"
)

func main() {
	mcp.New(&mcp.Config{
		Logger: log.InferLogger("mcp"),
	}).Serve()
}
