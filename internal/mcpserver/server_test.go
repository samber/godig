package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	pkggodev "github.com/samber/go-pkggodev-client"
	"github.com/samber/godig/internal/dispatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, res)
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no text content in result")
	return ""
}

func newHandler(t *testing.T, h http.HandlerFunc) *handler {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := pkggodev.New(pkggodev.WithBaseURL(srv.URL + "/v1beta"))
	require.NoError(t, err)
	return &handler{d: dispatch.New(c)}
}

func TestPackageTool(t *testing.T) {
	t.Parallel()
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/package/github.com/samber/lo", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"github.com/samber/lo","name":"lo"}`))
	})

	var req mcp.CallToolRequest
	req.Params.Name = "package-info"
	req.Params.Arguments = map[string]any{"path": "github.com/samber/lo"}

	res, err := h.handle("package-info")(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, textOf(t, res), "github.com/samber/lo")
}

func TestNewRegistersTools(t *testing.T) {
	t.Parallel()
	c, err := pkggodev.New()
	require.NoError(t, err)
	s := New("godig-test", "0.0.0", dispatch.New(c))
	require.NotNil(t, s)
}
