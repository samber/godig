package dispatch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pkggodev "github.com/samber/go-pkggodev-client"
	"github.com/samber/godig/internal/dispatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDispatcher(t *testing.T, h http.HandlerFunc) *dispatch.Dispatcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := pkggodev.New(pkggodev.WithBaseURL(srv.URL + "/v1beta"))
	require.NoError(t, err)
	return dispatch.New(c)
}

func TestInvoke_GetPackage(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/package/github.com/samber/lo", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("imports"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"github.com/samber/lo","name":"lo"}`))
	})

	out, err := d.Invoke(context.Background(), "package-info", map[string]any{
		"path":    "github.com/samber/lo",
		"imports": true,
	})
	require.NoError(t, err)
	b, _ := json.Marshal(out)
	assert.Contains(t, string(b), `"github.com/samber/lo"`)
}

func TestInvoke_GetSearch(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/search", r.URL.Path)
		assert.Equal(t, "slice", r.URL.Query().Get("q"))
		assert.Equal(t, "5", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	})

	_, err := d.Invoke(context.Background(), "search", map[string]any{
		"query": "slice",
		"limit": float64(5), // MCP delivers numbers as float64
	})
	require.NoError(t, err)
}

func TestInvoke_UnknownOperation(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := d.Invoke(context.Background(), "nope", map[string]any{})
	require.Error(t, err)
}
