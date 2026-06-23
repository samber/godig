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

// newDispatcherAPIAndProxy wires both a mock pkg.go.dev API and a mock Go module
// proxy, needed by operations that combine the two (e.g. `module info` with
// WithSize: module metadata from the API, zip size from the proxy).
func newDispatcherAPIAndProxy(t *testing.T, api, proxy http.HandlerFunc) *dispatch.Dispatcher {
	t.Helper()
	asrv := httptest.NewServer(api)
	t.Cleanup(asrv.Close)
	psrv := httptest.NewServer(proxy)
	t.Cleanup(psrv.Close)
	c, err := pkggodev.New(pkggodev.WithBaseURL(asrv.URL+"/v1beta"), pkggodev.WithGoproxy(psrv.URL))
	require.NoError(t, err)
	return dispatch.New(c)
}

func TestInvoke_PackageImports(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/package/github.com/samber/lo", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("imports"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"github.com/samber/lo","name":"lo","imports":["fmt","strings"]}`))
	})

	out, err := d.Invoke(context.Background(), "package-imports", map[string]any{
		"path": "github.com/samber/lo",
	})
	require.NoError(t, err)
	// The result is just the list of imports, not the whole package payload.
	imports, ok := out.([]string)
	require.True(t, ok, "expected []string, got %T", out)
	assert.Equal(t, []string{"fmt", "strings"}, imports)
}

func TestInvoke_PackageInfo_ProjectsMetadataOnly(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/package/github.com/samber/lo", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// API returns heavy fields too; package info must not leak them.
		_, _ = w.Write([]byte(`{"path":"github.com/samber/lo","name":"lo","version":"v1.2.3",` +
			`"docs":"# huge docs","imports":["fmt"],"licenses":[{"types":["MIT"]}]}`))
	})

	out, err := d.Invoke(context.Background(), "package-info", map[string]any{
		"path": "github.com/samber/lo",
	})
	require.NoError(t, err)

	// Allow-list projection: only metadata fields, no docs/imports/licenses.
	b, err := json.Marshal(out)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"name":"lo"`)
	assert.Contains(t, s, `"version":"v1.2.3"`)
	assert.NotContains(t, s, "docs")
	assert.NotContains(t, s, "imports")
	assert.NotContains(t, s, "licenses")
}

func TestInvoke_GetSearch(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/search", r.URL.Path)
		assert.Equal(t, "slice", r.URL.Query().Get("q"))
		assert.Equal(t, "5", r.URL.Query().Get("limit"))
		// search is the only operation that forwards --symbol as a query filter.
		assert.Equal(t, "Map", r.URL.Query().Get("symbol"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	})

	_, err := d.Invoke(context.Background(), "search", map[string]any{
		"query":  "slice",
		"symbol": "Map",
		"limit":  float64(5), // MCP delivers numbers as float64
	})
	require.NoError(t, err)
}

func TestInvoke_Symbol_RequiresSymbol(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("client must not be called when the symbol argument is missing")
	})
	_, err := d.Invoke(context.Background(), "symbol-doc", map[string]any{
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

func TestInvoke_SymbolDoc(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasPrefix(r.URL.Path, "/v1beta/package/"), r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"path": "demo/pkg",
			"name": "demo",
			"docs": packageDocFixture(),
		})
		_, _ = w.Write(body)
	})

	out, err := d.Invoke(context.Background(), "symbol-doc", map[string]any{
		"path":   "demo/pkg",
		"symbol": "Foo",
	})
	require.NoError(t, err)

	// symbol doc returns the full symbol (signature + doc), without examples.
	sym, ok := out.(*pkggodev.Symbol)
	require.True(t, ok, "expected *pkggodev.Symbol, got %T", out)
	assert.Equal(t, "Foo", sym.Name)
	assert.Contains(t, sym.Signature, "func Foo")
	assert.Empty(t, sym.Examples, "symbol doc must not include examples")
}

func TestInvoke_SymbolExamples(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasPrefix(r.URL.Path, "/v1beta/package/"), r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"path": "demo/pkg",
			"name": "demo",
			"docs": packageDocFixture(),
		})
		_, _ = w.Write(body)
	})

	out, err := d.Invoke(context.Background(), "symbol-examples", map[string]any{
		"path":   "demo/pkg",
		"symbol": "Foo",
	})
	require.NoError(t, err)

	// symbol examples returns just the symbol's examples (a slice).
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

func TestInvoke_ModuleInfo_SizeAndGoVersion(t *testing.T) {
	t.Parallel()
	d := newDispatcherAPIAndProxy(t,
		func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1beta/module/github.com/samber/lo", r.URL.Path)
			// WithSize must be requested at the proxy, not the API.
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"path":"github.com/samber/lo","version":"v1.2.3",`+
				`"goModContents":"module github.com/samber/lo\n\ngo 1.21\n","hasGoMod":true}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Size comes from a HEAD on the module zip's Content-Length.
			assert.Equal(t, http.MethodHead, r.Method)
			assert.True(t, strings.HasSuffix(r.URL.Path, "/github.com/samber/lo/@v/v1.2.3.zip"), r.URL.Path)
			w.Header().Set("Content-Length", "4096")
			w.WriteHeader(http.StatusOK)
		},
	)

	out, err := d.Invoke(context.Background(), "module-info", map[string]any{
		"path": "github.com/samber/lo",
	})
	require.NoError(t, err)

	b, err := json.Marshal(out)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"goVersion":"1.21"`)
	assert.Contains(t, s, `"size":4096`)
}

// TestInvoke_ModuleInfo_SizeSkippedWithoutProxy verifies `module info` still
// succeeds (omitting size) when no module proxy is usable, while goVersion —
// derived from the go.mod the API returns — is still present.
func TestInvoke_ModuleInfo_SizeSkippedWithoutProxy(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"path":"github.com/samber/lo","version":"v1.2.3",`+
			`"goModContents":"module github.com/samber/lo\n\ngo 1.21\n","hasGoMod":true}`)
	})

	out, err := d.Invoke(context.Background(), "module-info", map[string]any{
		"path": "github.com/samber/lo",
	})
	require.NoError(t, err)

	b, err := json.Marshal(out)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"goVersion":"1.21"`)
	assert.NotContains(t, s, `"size"`)
}

// TestInvoke_ModuleInfo_GoVersionBackfill covers the common production case
// where pkg.go.dev returns no go.mod: `module info` must backfill goVersion from
// the module proxy's go.mod (the "go" directive).
func TestInvoke_ModuleInfo_GoVersionBackfill(t *testing.T) {
	t.Parallel()
	goMod := "module github.com/samber/lo\n\ngo 1.18\n"
	d := newDispatcherAPIAndProxy(t,
		func(w http.ResponseWriter, _ *http.Request) {
			// No goModContents -> goVersion absent from the API response.
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"path":"github.com/samber/lo","version":"v1.2.3","hasGoMod":true}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/@v/v1.2.3.zip"):
				w.Header().Set("Content-Length", "4096")
				w.WriteHeader(http.StatusOK)
			case strings.HasSuffix(r.URL.Path, "/github.com/samber/lo/@latest"):
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"Version":"v1.2.3"}`)
			case strings.HasSuffix(r.URL.Path, "/github.com/samber/lo/@v/v1.2.3.mod"):
				_, _ = io.WriteString(w, goMod)
			default:
				http.NotFound(w, r)
			}
		},
	)

	out, err := d.Invoke(context.Background(), "module-info", map[string]any{
		"path": "github.com/samber/lo",
	})
	require.NoError(t, err)

	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"goVersion":"1.18"`)
}

func TestInvoke_Dependencies(t *testing.T) {
	t.Parallel()
	goMod := "module github.com/samber/do\n\ngo 1.18\n\n" +
		"require github.com/stretchr/testify v1.8.0\n\n" +
		"require golang.org/x/sync v0.1.0 // indirect\n"
	d := newDispatcherWithProxy(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/github.com/samber/do/@latest"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Version":"v1.6.0"}`)
		case strings.HasSuffix(r.URL.Path, "/github.com/samber/do/@v/v1.6.0.mod"):
			_, _ = io.WriteString(w, goMod)
		default:
			http.NotFound(w, r)
		}
	})

	out, err := d.Invoke(context.Background(), "dependencies", map[string]any{
		"path": "github.com/samber/do",
	})
	require.NoError(t, err)

	b, err := json.Marshal(out)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"version":"v1.6.0"`)
	assert.Contains(t, s, `"goVersion":"1.18"`)
	assert.Contains(t, s, `github.com/stretchr/testify`)
	assert.Contains(t, s, `"indirect":true`)
}

func TestInvoke_UnknownOperation(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := d.Invoke(context.Background(), "nope", map[string]any{})
	require.Error(t, err)
}

// A non-positive limit is a caller mistake (it would otherwise be silently
// treated as "unlimited"); it must error before any network call is made.
func TestInvoke_RejectsNonPositiveLimit(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected for an invalid limit, got %s", r.URL.Path)
	})
	for _, lim := range []any{0, -5, float64(0), float64(-1), float64(1.5)} {
		_, err := d.Invoke(context.Background(), "versions", map[string]any{
			"path":  "github.com/samber/lo",
			"limit": lim,
		})
		require.Error(t, err, "limit %v", lim)
		assert.Contains(t, err.Error(), "limit must be a positive integer")
	}
}

// A symbol with no examples must yield a non-nil empty slice so it renders as an
// empty list rather than a bare "null".
func TestInvoke_SymbolExamples_EmptyNotNil(t *testing.T) {
	t.Parallel()
	d := newDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Doc with a symbol but no Example blocks.
		body, _ := json.Marshal(map[string]any{
			"path": "demo/pkg",
			"name": "demo",
			"docs": "## func Foo\n\n```go\nfunc Foo()\n```\n",
		})
		_, _ = w.Write(body)
	})

	out, err := d.Invoke(context.Background(), "symbol-examples", map[string]any{
		"path":   "demo/pkg",
		"symbol": "Foo",
	})
	require.NoError(t, err)
	examples, ok := out.([]pkggodev.Example)
	require.True(t, ok, "expected []pkggodev.Example, got %T", out)
	assert.NotNil(t, examples)
	assert.Empty(t, examples)
}
