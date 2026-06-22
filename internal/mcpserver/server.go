// Package mcpserver exposes the pkg.go.dev operations as MCP tools over stdio.
package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/godig/internal/dispatch"
)

// handler adapts the dispatcher (or a caching decorator around it) to MCP tool
// handlers.
type handler struct {
	d invoker
}

// Option configures the MCP server built by New.
type Option func(*config)

// config holds the resolved options for New.
type config struct {
	cacheTTL  time.Duration
	cacheSize int
}

// WithCache enables an in-memory result cache in front of the dispatcher. A
// ttl <= 0 (or size <= 0) leaves caching disabled. Only MCP tool calls are
// cached; the CLI invokes the dispatcher directly.
func WithCache(ttl time.Duration, size int) Option {
	return func(c *config) {
		c.cacheTTL = ttl
		c.cacheSize = size
	}
}

// handle returns an MCP tool handler bound to the given operationId.
func (h *handler) handle(opID string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slog.Debug("mcp tool call", "tool", opID)
		out, err := h.d.Invoke(ctx, opID, req.GetArguments())
		if err != nil {
			slog.Error("mcp tool failed", "tool", opID, "err", err)
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	}
}

// New builds an MCP server with all pkg.go.dev tools registered. By default tool
// calls go straight to the dispatcher; pass WithCache to memoise results.
func New(name, version string, d *dispatch.Dispatcher, opts ...Option) *server.MCPServer {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	var inv invoker = d
	if cfg.cacheTTL > 0 && cfg.cacheSize > 0 {
		inv = newCachingInvoker(d, cfg.cacheTTL, cfg.cacheSize)
	}

	s := server.NewMCPServer(name, version, server.WithToolCapabilities(false))
	registerTools(s, &handler{d: inv})
	return s
}

// ServeStdio runs the server over stdio (blocking). This is the default
// transport used when an MCP client launches the binary directly.
func ServeStdio(s *server.MCPServer) error {
	return server.ServeStdio(s)
}

// ServeHTTP runs the server over streamable HTTP on addr (blocking). The MCP
// endpoint is exposed at /mcp.
func ServeHTTP(s *server.MCPServer, addr string) error {
	return server.NewStreamableHTTPServer(s).Start(addr)
}
