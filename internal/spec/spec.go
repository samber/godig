// Package spec is the hand-written catalog of pkg.go.dev operations exposed by
// godig's CLI and MCP server. Commands and tools are built from this table and
// call the typed go-pkggodev-client.
//
// An operation's Key (Group+"-"+Name, or just Name) is used as the MCP tool
// name and the dispatch key. Grouped operations become CLI subcommands of their
// group (e.g. Group "package", Name "info" -> `godig package info`).
package spec

// ParamType is the kind of an optional query parameter.
type ParamType string

// String, Int and Bool are the supported parameter kinds.
const (
	String ParamType = "string"
	Int    ParamType = "int"
	Bool   ParamType = "bool"
)

// Command group names (parents with subcommands).
const (
	groupPackage = "package"
	groupModule  = "module"
	groupSymbol  = "symbol"
)

// Param describes one optional query parameter of an operation.
type Param struct {
	Name string
	Type ParamType
	Desc string
}

// Operation describes one pkg.go.dev operation.
type Operation struct {
	Group    string // "" for a top-level command, otherwise the parent command
	Name     string // command/subcommand name
	Short    string
	Arg      string // required positional argument name ("" = none), e.g. "path" or "query"
	ArgDesc  string // description of the positional argument
	Arg2     string // optional second required positional argument ("" = none); requires Arg
	Arg2Desc string // description of the second positional argument
	Params   []Param
}

// Key is the dispatch key and MCP tool name (e.g. "package-info", "search").
func (o Operation) Key() string {
	if o.Group == "" {
		return o.Name
	}
	return o.Group + "-" + o.Name
}

// Common parameter definitions, reused across operations.
var (
	pVersion = Param{"version", String, "Module version (semver, 'latest', 'master' or 'main')"}
	pModule  = Param{"module", String, "Module path"}
	pLimit   = Param{"limit", Int, "Maximum number of items to return"}
	pFilter  = Param{"filter", String, `Filter results with a Go boolean expression over each item's fields, e.g. 'hasPrefix(packagePath, "github.com/")' or 'kind == "Function"'. Field names are the item's JSON keys (see README "Filters").`}
	pGOOS    = Param{"goos", String, "GOOS documentation build context"}
	pGOARCH  = Param{"goarch", String, "GOARCH documentation build context"}
)

const (
	argName     = "path"                          // common positional argument name
	argDesc     = "Package or module import path" // common positional argument description
	paramSymbol = "symbol"                        // common symbol parameter name
)

// GroupShort returns the short description of a parent command.
func GroupShort(group string) string {
	switch group {
	case groupPackage:
		return "Inspect a package (info, imports, examples, licenses, doc)"
	case groupModule:
		return "Inspect a module (info, licenses, readme)"
	case groupSymbol:
		return "Inspect a single exported symbol (doc, examples)"
	default:
		return group
	}
}

// Operations is the catalog consumed by the CLI and the MCP server.
//
// Token-efficiency note for agents: `overview` answers most "what is X / is it
// maintained / vulnerable / latest version" questions in ONE call with a compact
// payload. Reach for `doc`/`examples`/`module readme`/`licenses` only when the
// full (large) text is actually needed.
var Operations = []Operation{
	{
		Name:    "overview",
		Short:   "One-call compact summary of a Go package: metadata, latest + recent versions, license types and vulnerabilities. Start here.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pVersion},
	},
	{
		Name:    "search",
		Short:   "Find Go packages by query (optionally restricted to ones exporting a symbol).",
		Arg:     "query",
		ArgDesc: "Search query matching packages",
		Params: []Param{
			{paramSymbol, String, "Restrict results to packages exporting this symbol"},
			pLimit, pFilter,
		},
	},
	// package subcommands
	{
		Group:   groupPackage,
		Name:    "info",
		Short:   "Go package metadata (name, synopsis, latest version, module).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pModule, pVersion},
	},
	{
		Group:   groupPackage,
		Name:    "imports",
		Short:   "List the packages that a Go package imports.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pModule, pVersion},
	},
	{
		Group:   groupPackage,
		Name:    "doc",
		Short:   "Full Go package documentation. LARGE — fetch only when you need API details.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params: []Param{
			{"format", String, "Documentation format: md|text|html|markdown"},
			pModule, pVersion, pGOOS, pGOARCH,
		},
	},
	{
		Group:   groupPackage,
		Name:    "examples",
		Short:   "Go package documentation including runnable examples. LARGE (use --symbol to scope).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params: []Param{
			{paramSymbol, String, "Show examples for this symbol only (e.g. 'Map' or 'Type.Method')"},
			pModule, pVersion, pGOOS, pGOARCH,
		},
	},
	{
		Group:   groupPackage,
		Name:    "licenses",
		Short:   "Go package license files (full text). LARGE — for SPDX types use overview.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pModule, pVersion},
	},
	// module subcommands
	{
		Group:   groupModule,
		Name:    "info",
		Short:   "Go module metadata (latest version, repo URL, commit time).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pVersion},
	},
	{
		Group:   groupModule,
		Name:    "licenses",
		Short:   "Go module license files (full text). LARGE — for SPDX types use overview.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pVersion},
	},
	{
		Group:   groupModule,
		Name:    "readme",
		Short:   "Go module README (full Markdown). LARGE.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pVersion},
	},
	{
		Name:    "imported-by",
		Short:   "List Go packages that import this package (can be long; use --limit).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pModule, pVersion, pLimit, pFilter},
	},
	{
		Name:    "packages",
		Short:   "List the Go packages contained in a module.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pVersion, pLimit, pFilter},
	},
	{
		Name:    "versions",
		Short:   "List a Go module's versions, newest first (can be long; use --limit).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pLimit, pFilter},
	},
	{
		Name:    "dependencies",
		Short:   "Export a Go module's dependencies from its go.mod (requires, replaces, excludes, go directive).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pVersion},
	},
	{
		Name:    "major-versions",
		Short:   "List a Go module's major versions (v1, v2, v3 ...), which live as separate modules.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params: []Param{
			pLimit, pFilter,
			{"exclude-pseudo", Bool, "Drop majors whose latest version is a pseudo-version"},
		},
	},
	{
		Name:    "symbols",
		Short:   "List a Go package's exported symbols (types, funcs, methods).",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pModule, pVersion, pGOOS, pGOARCH, pLimit, pFilter},
	},
	// symbol subcommands
	{
		Group:    groupSymbol,
		Name:     "doc",
		Short:    "Documentation for a single exported symbol (signature + doc). Token-efficient vs package doc.",
		Arg:      argName,
		ArgDesc:  argDesc,
		Arg2:     paramSymbol,
		Arg2Desc: "Exported symbol, e.g. 'Map' or 'Type.Method'",
		Params:   []Param{pModule, pVersion, pGOOS, pGOARCH},
	},
	{
		Group:    groupSymbol,
		Name:     "examples",
		Short:    "Runnable examples for a single exported symbol. Token-efficient vs package examples.",
		Arg:      argName,
		ArgDesc:  argDesc,
		Arg2:     paramSymbol,
		Arg2Desc: "Exported symbol, e.g. 'Map' or 'Type.Method'",
		Params:   []Param{pModule, pVersion, pGOOS, pGOARCH},
	},
	{
		Name:    "vulns",
		Short:   "Known vulnerabilities of a Go module or package.",
		Arg:     argName,
		ArgDesc: argDesc,
		Params:  []Param{pModule, pVersion, pLimit, pFilter},
	},
}
