// Package mcpserver exposes the pkg.go.dev operations as MCP tools over stdio.
package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/godig/internal/dispatch"
)

// handler adapts the dispatcher to MCP tool handlers.
type handler struct {
	d *dispatch.Dispatcher
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
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	}
}

// New builds an MCP server with all pkg.go.dev tools registered.
func New(name, version string, d *dispatch.Dispatcher) *server.MCPServer {
	s := server.NewMCPServer(name, version, server.WithToolCapabilities(false))
	registerTools(s, &handler{d: d})
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
