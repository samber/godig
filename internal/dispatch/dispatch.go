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
	"time"

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
		return &apiError{msg: "not found: " + target, err: err}
	}
	if msg := apiMessage(status.Payload); msg != "" {
		return &apiError{msg: fmt.Sprintf("pkg.go.dev: %s (HTTP %d)", msg, status.StatusCode), err: err}
	}
	return &apiError{msg: fmt.Sprintf("pkg.go.dev returned HTTP %d", status.StatusCode), err: err}
}

// apiError carries a human-friendly message for display while preserving the
// underlying client error, so callers can still inspect it with errors.Is/As.
type apiError struct {
	msg string
	err error
}

func (e *apiError) Error() string { return e.msg }
func (e *apiError) Unwrap() error { return e.err }

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
	if strings.HasPrefix(name, "symbol-") {
		return d.routeSymbol(ctx, name, path, a, opts)
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
		// WithSymbol only applies to search (it restricts results to packages
		// exporting the symbol); the symbol routes pass it positionally instead.
		if sym := str(a, "symbol"); sym != "" {
			opts = append(opts, pkggodev.WithSymbol(sym))
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
	case "vulns":
		return collectN(d.c.AllVulns(ctx, path, opts...), limit)
	default:
		return nil, fmt.Errorf("unknown operation %q", name)
	}
}

// PackageInfo is the metadata-only projection returned by `package info`.
// Building it from an allow-list (rather than nilling heavy fields on
// pkggodev.Package) keeps the payload compact and fails safe: a new field on the
// client struct is never leaked unless it is explicitly added here.
type PackageInfo struct {
	Path              string `json:"path"`
	ModulePath        string `json:"modulePath,omitempty"`
	Name              string `json:"name,omitempty"`
	Synopsis          string `json:"synopsis,omitempty"`
	Version           string `json:"version,omitempty"`
	Goos              string `json:"goos,omitempty"`
	Goarch            string `json:"goarch,omitempty"`
	IsLatest          bool   `json:"isLatest"`
	IsRedistributable bool   `json:"isRedistributable"`
	IsStandardLibrary bool   `json:"isStandardLibrary"`
}

func newPackageInfo(p *pkggodev.Package) PackageInfo {
	return PackageInfo{
		Path:              p.Path,
		ModulePath:        p.ModulePath,
		Name:              p.Name,
		Synopsis:          p.Synopsis,
		Version:           p.Version,
		Goos:              p.Goos,
		Goarch:            p.Goarch,
		IsLatest:          p.IsLatest,
		IsRedistributable: p.IsRedistributable,
		IsStandardLibrary: p.IsStandardLibrary,
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
		return newPackageInfo(p), nil
	case "package-imports":
		p, err := d.c.Package(ctx, path, append(opts, pkggodev.WithImports())...)
		if err != nil {
			return nil, err
		}
		imports := p.Imports
		if imports == nil {
			imports = []string{}
		}
		return imports, nil
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
			return d.symbolExamples(ctx, path, sym, opts)
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

// ModuleInfo is the metadata-only projection returned by `module info`.
// See PackageInfo for the allow-list rationale.
type ModuleInfo struct {
	Path              string    `json:"path"`
	Version           string    `json:"version,omitempty"`
	RepoURL           string    `json:"repoUrl,omitempty"`
	CommitTime        time.Time `json:"commitTime,omitzero"`
	HasGoMod          bool      `json:"hasGoMod"`
	IsLatest          bool      `json:"isLatest"`
	IsRedistributable bool      `json:"isRedistributable"`
	IsStandardLibrary bool      `json:"isStandardLibrary"`
}

func newModuleInfo(m *pkggodev.Module) ModuleInfo {
	return ModuleInfo{
		Path:              m.Path,
		Version:           m.Version,
		RepoURL:           m.RepoURL,
		CommitTime:        m.CommitTime,
		HasGoMod:          m.HasGoMod,
		IsLatest:          m.IsLatest,
		IsRedistributable: m.IsRedistributable,
		IsStandardLibrary: m.IsStandardLibrary,
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
		return newModuleInfo(m), nil
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

// routeSymbol handles the `symbol` subcommands for a single exported symbol:
// `doc` returns the signature + documentation, `examples` returns just the
// symbol's runnable examples. The symbol argument is required. The doc format is
// fixed by the client (it parses the package's markdown documentation).
func (d *Dispatcher) routeSymbol(ctx context.Context, name, path string, a map[string]any, opts []pkggodev.Option) (any, error) {
	sym := str(a, "symbol")
	if sym == "" {
		return nil, errors.New("symbol requires a symbol argument")
	}
	switch name {
	case "symbol-doc":
		return d.c.Symbol(ctx, path, sym, opts...)
	case "symbol-examples":
		return d.symbolExamples(ctx, path, sym, opts)
	default:
		return nil, fmt.Errorf("unknown operation %q", name)
	}
}

// symbolExamples returns just the runnable examples of a single exported symbol.
// Symbol() always parses the package's markdown doc, so only WithExamples matters
// here (WithDoc is a no-op on this call). Shared by `symbol examples` and
// `package examples --symbol`.
func (d *Dispatcher) symbolExamples(ctx context.Context, path, sym string, opts []pkggodev.Option) ([]pkggodev.Example, error) {
	s, err := d.c.Symbol(ctx, path, sym, append(opts, pkggodev.WithExamples())...)
	if err != nil {
		return nil, err
	}
	return s.Examples, nil
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
// secondary lookups are best-effort and skipped (logged at debug) on error.
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
	} else {
		slog.Debug("overview: module lookup skipped", "module", modulePath, "err", e)
	}
	if page, e := d.c.Versions(ctx, modulePath, pkggodev.WithLimit(10)); e == nil {
		for _, v := range page.Items {
			ov.RecentVersions = append(ov.RecentVersions, v.Version)
		}
	} else {
		slog.Debug("overview: versions lookup skipped", "module", modulePath, "err", e)
	}
	if page, e := d.c.Vulns(ctx, path); e == nil {
		for _, v := range page.Items {
			ov.Vulnerabilities = append(ov.Vulnerabilities, v.ID)
		}
	} else {
		slog.Debug("overview: vulns lookup skipped", "path", path, "err", e)
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
		{"goos", pkggodev.WithGOOS},
		{"goarch", pkggodev.WithGOARCH},
	}
	for _, s := range stringOpts {
		if v := str(a, s.key); v != "" {
			o = append(o, s.fn(v))
		}
	}

	if n := intOf(a, "limit"); n > 0 {
		o = append(o, pkggodev.WithLimit(n))
	}

	// Facet options (doc/examples/imports/licenses/readme) are applied explicitly
	// by the routePackage/routeSymbol handlers, not derived from generic flags;
	// exclude-pseudo is the only bool flag left in the generic path.
	if boolOf(a, "exclude-pseudo") {
		o = append(o, pkggodev.WithExcludePseudo())
	}

	return o
}

// --- argument coercion helpers (tolerate CLI strings and MCP JSON types) ---

func str(a map[string]any, key string) string {
	switch v := a[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		// JSON numbers decode as float64; render whole numbers without a
		// fractional part (e.g. a version passed as 2 instead of "2").
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		// Unexpected composite type for a string field: avoid leaking a Go-syntax
		// representation (fmt.Sprint) into an API path/query.
		return ""
	}
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
