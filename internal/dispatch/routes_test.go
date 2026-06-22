package dispatch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pkggodev "github.com/samber/go-pkggodev-client"
	"github.com/samber/godig/internal/dispatch"
	"github.com/samber/godig/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInvoke_EveryOperationRoutes guards against spec/dispatch drift: every
// operation in the hand-written catalog must be routable by the dispatcher.
// A catalog entry with no matching dispatch case would still compile and
// register a CLI command and an MCP tool, then fail only at call time with
// "unknown operation". Other errors (e.g. decode failures against the stub
// server) are irrelevant here — we assert solely that routing resolves.
func TestInvoke_EveryOperationRoutes(t *testing.T) {
	t.Parallel()

	empty := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
	api := httptest.NewServer(http.HandlerFunc(empty))
	t.Cleanup(api.Close)
	// major-versions probes the Go module proxy directly (not the API base URL);
	// point it at a stub too so the test never reaches the network.
	proxy := httptest.NewServer(http.HandlerFunc(empty))
	t.Cleanup(proxy.Close)

	c, err := pkggodev.New(
		pkggodev.WithBaseURL(api.URL+"/v1beta"),
		pkggodev.WithGoproxy(proxy.URL),
	)
	require.NoError(t, err)
	d := dispatch.New(c)

	for _, op := range spec.Operations {
		t.Run(op.Key(), func(t *testing.T) {
			t.Parallel()
			// Provide every required positional so routing is reached for ops
			// that validate args before dispatching (search, symbol-*).
			args := map[string]any{"path": "example.com/x", "query": "x", "symbol": "X"}
			_, err := d.Invoke(context.Background(), op.Key(), args)
			if err != nil {
				assert.NotContains(t, err.Error(), "unknown operation",
					"operation %q is in the catalog but has no dispatch route", op.Key())
			}
		})
	}
}
