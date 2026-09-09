package mcpserver

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/api/authentication"
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
)

func NewHandler(db database.Database, logger *log.Logger) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "nautilus",
		Version: config.Get("APP_VERSION", "development"),
	}, &mcp.ServerOptions{Logger: logger.Logger})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "hello_world",
		Description: "Return a greeting to verify the Nautilus MCP connection.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, helloWorld)

	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		PropagateRequestCancellation: true,
		Logger:                       logger.Logger,
	})
	r := mux.New(mux.Config{Middleware: []mux.Middleware{
		middleware.AccessLog,
		middleware.Recover,
		authentication.RequireAPIKey(db),
	}})
	r.Handle(http.MethodPost, "/mcp", http.NewCrossOriginProtection().Handler(transport))
	return r
}

func helloWorld(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Hello, world!"}},
	}, nil, nil
}
