package mcp

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/kms"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/oauth"
	"nautilus/internal/objectstore"
)

func NewHandler(db database.Database, store objectstore.Store, keys kms.KeyManager, logger *log.Logger) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "nautilus",
		Version: config.Get("APP_VERSION", "development"),
	}, &mcp.ServerOptions{Logger: logger.Logger})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "hello_world",
		Description: "Return a greeting to verify the Nautilus MCP connection.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, helloWorld)
	docs := &documentTools{db: db, store: store, keys: keys}
	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_documents", Description: "List documents in your organization, newest first. Use next_cursor to continue. Requires read scope.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, docs.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_document", Description: "Get a document's metadata by ID, including filename, status, page count, and SHA-256. Requires read scope.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, docs.get)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "read_document", Description: "Read extracted UTF-8 document text. Use next_offset while has_more is true to read the entire document. Text may be unavailable while OCR is pending. Requires read scope.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, docs.read)

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
	}})
	r.Get("/.well-known/oauth-authorization-server", oauth.Metadata)
	r.Get("/.well-known/oauth-protected-resource", resourceMetadata)
	r.Get("/.well-known/oauth-protected-resource/mcp", resourceMetadata)
	r.Use(middleware.MCPAuth(db, oauth.Issuer()))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		})
	})
	r.Handle(http.MethodPost, "/mcp", http.NewCrossOriginProtection().Handler(transport))
	return r
}

func helloWorld(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Hello, world!"}},
	}, nil, nil
}
