// Package dispatch bridges a generic (operation name, args) call to the typed
// go-pkggodev-client public API. It is the shared seam between the CLI and the
// MCP server (both driven by internal/spec) and the pkg.go.dev client.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ogen-go/ogen/validate"
	pkggodev "github.com/samber/go-pkggodev-client"
)

// Dispatcher routes operations to the typed pkg.go.dev client.
type Dispatcher struct {
	c *pkggodev.Client
}

// New creates a Dispatcher backed by the given client.
func New(c *pkggodev.Client) *Dispatcher { return &Dispatcher{c: c} }

// Invoke calls the operation identified by its name (see internal/spec) with
// the given arguments, returning the typed (JSON-serialisable) response. HTTP
// status errors are translated into friendly messages.
func (d *Dispatcher) Invoke(ctx context.Context, name string, a map[string]any) (any, error) {
	res, err := d.route(ctx, name, a)
	return res, friendlyError(err, name, str(a, "path"), str(a, "version"))
}

// friendlyError turns ogen's "unexpected status code" errors into readable
// messages (notably 404 → "not found: <path>@<version>").
func friendlyError(err error, name, path, version string) error {
	if err == nil {
		return nil
	}
	var status *validate.UnexpectedStatusCodeError
	if !errors.As(err, &status) {
		return err
	}
	if status.StatusCode == http.StatusNotFound {
		target := path
		if target == "" {
			target = name
		}
		if version != "" {
			target += "@" + version
		}
		return fmt.Errorf("not found: %s", target)
	}
	if msg := apiMessage(status.Payload); msg != "" {
		return fmt.Errorf("pkg.go.dev: %s (HTTP %d)", msg, status.StatusCode)
	}
	return fmt.Errorf("pkg.go.dev returned HTTP %d", status.StatusCode)
}

// apiMessage extracts the pkg.go.dev error message from a buffered error
// response body (the ogen client retains it), returning "" if unavailable.
func apiMessage(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &payload) != nil {
		return ""
	}
	return payload.Message
}

func (d *Dispatcher) route(ctx context.Context, name string, a map[string]any) (any, error) {
	path := str(a, "path")
	opts := optionsFrom(a)
	limit := intOf(a, "limit")
	slog.Info("invoking operation", "operation", name, "path", path, "options", len(opts))

	if strings.HasPrefix(name, "package-") {
		return d.routePackage(ctx, name, path, a, opts)
	}
	if strings.HasPrefix(name, "module-") {
		return d.routeModule(ctx, name, path, opts)
	}

	// Listing operations auto-paginate (AllX) and return the full slice of items,
	// so the table output shows rows and never a nextToken cursor. --limit caps
	// the total number of items returned.
	switch name {
	case "overview":
		return d.overview(ctx, path, opts)
	case "search":
		if str(a, "query") == "" {
			return nil, errors.New("search requires a query argument")
		}
		return collectN(d.c.AllSearch(ctx, opts...), limit)
	case "imported-by":
		paths, err := collectN(d.c.AllImportedBy(ctx, path, opts...), limit)
		if err != nil {
			return nil, err
		}
		return labelStrings(paths, "package"), nil
	case "packages":
		return collectN(d.c.AllPackages(ctx, path, opts...), limit)
	case "versions":
		return collectN(d.c.AllVersions(ctx, path, opts...), limit)
	case "major-versions":
		return d.majorVersions(ctx, path, opts)
	case "symbols":
		return collectN(d.c.AllSymbols(ctx, path, opts...), limit)
	case "symbol":
		return d.symbol(ctx, path, a, opts)
	case "vulns":
		return collectN(d.c.AllVulns(ctx, path, opts...), limit)
	default:
		return nil, fmt.Errorf("unknown operation %q", name)
	}
}

// routePackage handles the `package` subcommands, projecting the response to the
// requested facet only (no basic info on doc/examples/licenses).
func (d *Dispatcher) routePackage(ctx context.Context, name, path string, a map[string]any, opts []pkggodev.Option) (any, error) {
	switch name {
	case "package-info":
		p, err := d.c.Package(ctx, path, opts...)
		if err != nil {
			return nil, err
		}
		p.Docs = ""
		p.Licenses = nil
		return p, nil
	case "package-doc":
		format := str(a, "format")
		if format == "" {
			format = "md"
		}
		p, err := d.c.Package(ctx, path, append(opts, pkggodev.WithDoc(format))...)
		if err != nil {
			return nil, err
		}
		return p.Docs, nil
	case "package-examples":
		// Scoped to a single symbol when --symbol is given: return just that
		// symbol's examples instead of the whole (large) package examples blob.
		if sym := str(a, "symbol"); sym != "" {
			s, err := d.c.Symbol(ctx, path, sym, append(opts, pkggodev.WithDoc("md"), pkggodev.WithExamples())...)
			if err != nil {
				return nil, err
			}
			return s.Examples, nil
		}
		// The API embeds examples in the docs, which require a doc format.
		p, err := d.c.Package(ctx, path, append(opts, pkggodev.WithDoc("md"), pkggodev.WithExamples())...)
		if err != nil {
			return nil, err
		}
		return p.Docs, nil
	case "package-licenses":
		p, err := d.c.Package(ctx, path, append(opts, pkggodev.WithLicenses())...)
		if err != nil {
			return nil, err
		}
		return p.Licenses, nil
	default:
		return nil, fmt.Errorf("unknown operation %q", name)
	}
}

// routeModule handles the `module` subcommands.
func (d *Dispatcher) routeModule(ctx context.Context, name, path string, opts []pkggodev.Option) (any, error) {
	switch name {
	case "module-info":
		m, err := d.c.Module(ctx, path, opts...)
		if err != nil {
			return nil, err
		}
		m.Licenses = nil
		m.Readme = pkggodev.Readme{}
		m.GoModContents = ""
		return m, nil
	case "module-licenses":
		m, err := d.c.Module(ctx, path, append(opts, pkggodev.WithLicenses())...)
		if err != nil {
			return nil, err
		}
		return m.Licenses, nil
	case "module-readme":
		m, err := d.c.Module(ctx, path, append(opts, pkggodev.WithReadme())...)
		if err != nil {
			return nil, err
		}
		return m.Readme.Contents, nil
	default:
		return nil, fmt.Errorf("unknown operation %q", name)
	}
}

// symbol returns the documentation of a single exported symbol. The symbol
// argument is required; --format selects the doc rendering (md|text|html).
func (d *Dispatcher) symbol(ctx context.Context, path string, a map[string]any, opts []pkggodev.Option) (any, error) {
	sym := str(a, "symbol")
	if sym == "" {
		return nil, errors.New("symbol requires a symbol argument")
	}
	if f := str(a, "format"); f != "" {
		opts = append(opts, pkggodev.WithDoc(f))
	}
	return d.c.Symbol(ctx, path, sym, opts...)
}

// majorVersions lists a module's major versions. MajorVersions applies
// limit/filter/exclude-pseudo internally, so we return the items slice to render
// it like the other listing operations.
func (d *Dispatcher) majorVersions(ctx context.Context, path string, opts []pkggodev.Option) (any, error) {
	page, err := d.c.MajorVersions(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// Overview is a compact, token-efficient summary of a package, composed from
// several pkg.go.dev calls so an agent needs only one tool call.
type Overview struct {
	Path              string   `json:"path"`
	Name              string   `json:"name,omitempty"`
	Synopsis          string   `json:"synopsis,omitempty"`
	ModulePath        string   `json:"modulePath,omitempty"`
	LatestVersion     string   `json:"latestVersion,omitempty"`
	RepoURL           string   `json:"repoUrl,omitempty"`
	IsStandardLibrary bool     `json:"isStandardLibrary"`
	Licenses          []string `json:"licenses,omitempty"`        // SPDX types, not full text
	RecentVersions    []string `json:"recentVersions,omitempty"`  // up to 10, newest first
	Vulnerabilities   []string `json:"vulnerabilities,omitempty"` // vulnerability IDs
}

// overview composes package + module + versions + vulns into one compact result.
// The package lookup is authoritative (its error, e.g. 404, is returned); the
// secondary lookups are best-effort and silently skipped on error.
func (d *Dispatcher) overview(ctx context.Context, path string, opts []pkggodev.Option) (any, error) {
	pkg, err := d.c.Package(ctx, path, append(opts, pkggodev.WithLicenses())...)
	if err != nil {
		return nil, err
	}
	modulePath := pkg.ModulePath
	if modulePath == "" {
		modulePath = path
	}
	ov := &Overview{
		Path:              path,
		Name:              pkg.Name,
		Synopsis:          pkg.Synopsis,
		ModulePath:        modulePath,
		IsStandardLibrary: pkg.IsStandardLibrary,
		LatestVersion:     pkg.Version,
		Licenses:          licenseTypes(pkg.Licenses),
	}
	if mod, e := d.c.Module(ctx, modulePath); e == nil {
		ov.RepoURL = mod.RepoURL
		if mod.Version != "" {
			ov.LatestVersion = mod.Version
		}
	}
	if page, e := d.c.Versions(ctx, modulePath, pkggodev.WithLimit(10)); e == nil {
		for _, v := range page.Items {
			ov.RecentVersions = append(ov.RecentVersions, v.Version)
		}
	}
	if page, e := d.c.Vulns(ctx, path); e == nil {
		for _, v := range page.Items {
			ov.Vulnerabilities = append(ov.Vulnerabilities, v.ID)
		}
	}
	return ov, nil
}

// licenseTypes flattens license SPDX types into a unique, ordered list.
func licenseTypes(licenses []pkggodev.License) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range licenses {
		for _, t := range l.Types {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// labelStrings wraps a slice of strings into single-field rows so they render
// as a one-column table (and as self-describing JSON objects).
func labelStrings(items []string, field string) []map[string]string {
	out := make([]map[string]string, 0, len(items))
	for _, s := range items {
		out = append(out, map[string]string{field: s})
	}
	return out
}

// collectN drains an iterator into a slice, stopping after limit items when
// limit > 0 (limit <= 0 means collect everything). A yielded error aborts.
func collectN[T any](seq iter.Seq2[T, error], limit int) ([]T, error) {
	out := []T{} // non-nil so an empty result renders as "(no results)", not null
	for v, err := range seq {
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// optionsFrom translates the generic argument map (CLI flags / MCP tool args)
// into pkggodev call options. Methods ignore options that do not apply.
func optionsFrom(a map[string]any) []pkggodev.Option {
	var o []pkggodev.Option

	stringOpts := []struct {
		key string
		fn  func(string) pkggodev.Option
	}{
		{"version", pkggodev.WithVersion},
		{"module", pkggodev.WithModule},
		{"filter", pkggodev.WithFilter},
		{"query", pkggodev.WithQuery},
		{"symbol", pkggodev.WithSymbol},
		{"goos", pkggodev.WithGOOS},
		{"goarch", pkggodev.WithGOARCH},
	}
	for _, s := range stringOpts {
		if v := str(a, s.key); v != "" {
			o = append(o, s.fn(v))
		}
	}

	examples := boolOf(a, "examples")
	doc := str(a, "doc")
	if doc == "" && examples {
		// The API rejects examples without a documentation format (HTTP 400),
		// so default to markdown when the user asks for examples.
		doc = "md"
	}
	if doc != "" {
		o = append(o, pkggodev.WithDoc(doc))
	}
	if n := intOf(a, "limit"); n > 0 {
		o = append(o, pkggodev.WithLimit(n))
	}

	boolOpts := []struct {
		key string
		fn  func() pkggodev.Option
	}{
		{"examples", pkggodev.WithExamples},
		{"imports", pkggodev.WithImports},
		{"licenses", pkggodev.WithLicenses},
		{"readme", pkggodev.WithReadme},
		{"exclude-pseudo", pkggodev.WithExcludePseudo},
	}
	for _, b := range boolOpts {
		if boolOf(a, b.key) {
			o = append(o, b.fn())
		}
	}

	return o
}

// --- argument coercion helpers (tolerate CLI strings and MCP JSON types) ---

func str(a map[string]any, key string) string {
	v, ok := a[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func intOf(a map[string]any, key string) int {
	switch n := a[key].(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

func boolOf(a map[string]any, key string) bool {
	switch b := a[key].(type) {
	case bool:
		return b
	case string:
		v, _ := strconv.ParseBool(b)
		return v
	}
	return false
}
