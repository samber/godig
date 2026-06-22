package mcpserver

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/godig/internal/spec"
)

// registerTools registers one MCP tool per operation in the hand-written
// catalog (internal/spec), each wired to the go-pkggodev-client via the handler.
func registerTools(s *server.MCPServer, h *handler) {
	for _, op := range spec.Operations {
		opts := []mcp.ToolOption{mcp.WithDescription(op.Short)}
		if op.Arg != "" {
			opts = append(opts, mcp.WithString(op.Arg,
				mcp.Required(),
				mcp.Description(op.ArgDesc),
			))
		}
		if op.Arg2 != "" {
			opts = append(opts, mcp.WithString(op.Arg2,
				mcp.Required(),
				mcp.Description(op.Arg2Desc),
			))
		}
		for _, p := range op.Params {
			switch p.Type {
			case spec.Int:
				opts = append(opts, mcp.WithNumber(p.Name, mcp.Description(p.Desc)))
			case spec.Bool:
				opts = append(opts, mcp.WithBoolean(p.Name, mcp.Description(p.Desc)))
			case spec.String:
				opts = append(opts, mcp.WithString(p.Name, mcp.Description(p.Desc)))
			}
		}
		s.AddTool(mcp.NewTool(op.Key(), opts...), h.handle(op.Key()))
	}
}
