package dispatch_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// newDispatcherWithProxy backs MajorVersions with a mock Go module proxy
// (WithGoproxy), since MajorVersions probes the proxy directly rather than the
// pkg.go.dev API base URL.
func newDispatcherWithProxy(t *testing.T, proxy http.HandlerFunc) *dispatch.Dispatcher {
	t.Helper()
	api := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(api.Close)
	psrv := httptest.NewServer(proxy)
	t.Cleanup(psrv.Close)
	c, err := pkggodev.New(pkggodev.WithBaseURL(api.URL+"/v1beta"), pkggodev.WithGoproxy(psrv.URL))
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

func TestInvoke_Symbol_RequiresSymbol(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("client must not be called when the symbol argument is missing")
	})
	_, err := d.Invoke(context.Background(), "symbol", map[string]any{
		"path": "github.com/samber/lo",
	})
	require.Error(t, err)
}

// packageDocFixture is a minimal pkg.go.dev markdown doc with one function and
// its example, in the layout the client's single-symbol parser expects (the
// example is associated with the preceding function section).
func packageDocFixture() string {
	f := "```"
	return strings.Join([]string{
		"# package demo",
		"",
		"## Functions",
		"",
		f + "go",
		"func Foo(a int) int",
		f,
		"Foo doubles its argument.",
		"",
		"#### Example",
		"",
		f + "go",
		"{",
		"\tfmt.Println(Foo(2))",
		"}",
		f,
		"Output:",
		"",
		f,
		"4",
		f,
	}, "\n")
}

func TestInvoke_PackageExamples_BySymbol(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		// --symbol routes through Symbol(), which fetches the package doc.
		assert.True(t, strings.HasPrefix(r.URL.Path, "/v1beta/package/"), r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"path": "demo/pkg",
			"name": "demo",
			"docs": packageDocFixture(),
		})
		_, _ = w.Write(body)
	})

	out, err := d.Invoke(context.Background(), "package-examples", map[string]any{
		"path":   "demo/pkg",
		"symbol": "Foo",
	})
	require.NoError(t, err)

	// The result is the symbol's examples (a slice), not the whole docs string.
	examples, ok := out.([]pkggodev.Example)
	require.True(t, ok, "expected []pkggodev.Example, got %T", out)
	require.NotEmpty(t, examples)
	assert.Contains(t, examples[0].Code, "Foo(2)")
}

func TestInvoke_MajorVersions(t *testing.T) {
	t.Parallel()
	d := newDispatcherWithProxy(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/github.com/samber/do/@v/list"):
			_, _ = io.WriteString(w, "v1.0.0\nv1.6.0\n")
		case strings.HasSuffix(r.URL.Path, "/github.com/samber/do/@latest"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Version":"v1.6.0"}`)
		case strings.HasSuffix(r.URL.Path, "/github.com/samber/do/v2/@latest"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Version":"v2.0.0"}`)
		default: // higher majors are absent
			http.NotFound(w, r)
		}
	})

	out, err := d.Invoke(context.Background(), "major-versions", map[string]any{
		"path": "github.com/samber/do",
	})
	require.NoError(t, err)
	b, _ := json.Marshal(out)
	assert.Contains(t, string(b), "github.com/samber/do/v2")
	assert.Contains(t, string(b), `"major":"v2"`)
	assert.Contains(t, string(b), `"major":"v1"`)
}

func TestInvoke_UnknownOperation(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := d.Invoke(context.Background(), "nope", map[string]any{})
	require.Error(t, err)
}
