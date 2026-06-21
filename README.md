
# godig - CLI & MCP server for pkg.go.dev

[![tag](https://img.shields.io/github/tag/samber/godig.svg)](https://github.com/samber/godig/releases)
![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.25-%23007d9c)
[![GoDoc](https://godoc.org/github.com/samber/godig?status.svg)](https://pkg.go.dev/github.com/samber/godig)
![Build Status](https://github.com/samber/godig/actions/workflows/test.yml/badge.svg)
[![Go report](https://goreportcard.com/badge/github.com/samber/godig)](https://goreportcard.com/report/github.com/samber/godig)
[![Contributors](https://img.shields.io/github/contributors/samber/godig)](https://github.com/samber/godig/graphs/contributors)
[![License](https://img.shields.io/github/license/samber/godig)](./LICENSE)

`godig` is a CLI **and** an MCP server for exploring Go packages and modules via the [pkg.go.dev](https://pkg.go.dev) API: search, documentation, symbols, versions, importers and vulnerabilities — from your shell or from an AI agent.

Its commands and MCP tools are built from a small hand-written catalog ([`internal/spec`](internal/spec)) and call the typed [`go-pkggodev-client`](https://github.com/samber/go-pkggodev-client). All operations are read-only and need no authentication.

> [!TIP]
> Looking for a **Go library** instead of a CLI? Use [`samber/go-pkggodev-client`](https://github.com/samber/go-pkggodev-client) — the typed pkg.go.dev client that powers `godig`.

## 🚀 Install

```sh
go install github.com/samber/godig/cmd/godig@latest

# AI Agent Skill:
npx skills add https://github.com/samber/cc-skills-golang --skill golang-pkg-go-dev

# Register the MCP server into Claude Code (stdio)
claude mcp add pkg-go-dev -- godig mcp
```

Requires Go >= 1.25. See [Skill](#-skill) to also register the MCP server with your agent.

## 💡 Quick start

```sh
# Overview — one compact call: metadata, latest + recent versions, licenses, vulns
godig overview github.com/samber/ro

# Search
godig search "result option monad" --limit 5

# Package facets
godig package info github.com/samber/ro
godig package doc github.com/samber/ro --format md
godig package examples github.com/samber/ro
godig package licenses github.com/samber/ro

# Module facets
godig module info github.com/samber/ro
godig module readme github.com/samber/ro
godig module licenses github.com/samber/ro

# Lists (auto-paginated; --limit to cap, -o md for a Markdown table)
godig versions github.com/samber/ro -o md
godig packages github.com/samber/ro
godig imported-by github.com/samber/ro --limit 20
godig symbols github.com/samber/ro

# Filter (a Go boolean expression over item fields) and build context (goos/goarch)
godig versions github.com/samber/ro --filter 'hasPrefix(version, "v0.3")'
godig symbols github.com/samber/ro --goos linux --goarch amd64

# Vulnerabilities
godig vulns github.com/samber/ro

# Run as an MCP server (stdio; --transport http for HTTP)
godig mcp
```

Global flags: `-o, --output` (`table` default, `json`, `raw`, `md`), `--base-url`, `--timeout`,
`--log-level` (`debug|info|warn|error|off`, default `error`; logs go to **stderr**).
All flags can also be set via `GODIG_`-prefixed environment variables.

## 🧠 Commands

| Command                                   | Description                          |
| ----------------------------------------- | ------------------------------------ |
| `godig overview <path>`                   | One-call compact summary (start here)|
| `godig search <query> [--symbol <s>]`     | Search packages and symbols          |
| `godig package info <path>`               | Package metadata                     |
| `godig package doc <path> --format <fmt>` | Package documentation (md/text/html) |
| `godig package examples <path>`           | Documentation with examples          |
| `godig package licenses <path>`           | Package licenses                     |
| `godig module info <path>`                | Module metadata                      |
| `godig module licenses <path>`            | Module licenses                      |
| `godig module readme <path>`              | Module README                        |
| `godig packages <module>`                 | List a module's packages             |
| `godig versions <module>`                 | List module versions                 |
| `godig imported-by <path>`                | Packages that import a package       |
| `godig symbols <path>`                    | Exported symbols of a package        |
| `godig vulns <path>`                      | Known vulnerabilities                |
| `godig mcp`                               | Run the MCP server (stdio or http)   |

Run `godig <command> --help` (or `godig package --help`) for per-command flags. Each operation is
also exposed as an MCP tool (e.g. `overview`, `package-info`, `module-readme`).

**For AI agents (token-efficient):** start with `overview` — one call returns a compact summary
(no large docs). Fetch `doc`, `examples`, `module readme` or `licenses` only when the full text is
actually needed, and cap long lists (`versions`, `imported-by`) with `--limit`.

## 📫 MCP server

`godig mcp` runs an MCP server exposing one read-only tool per operation, over either transport.

**stdio** (default) — the client launches the binary on demand:

```sh
claude mcp add pkg-go-dev -- godig mcp
```

```json
{ "mcpServers": { "pkg-go-dev": { "command": "godig", "args": ["mcp"] } } }
```

**streamable HTTP** — a shared, long-running server at `/mcp` (`--addr`, default `:8080`):

```sh
godig mcp --transport http --addr :8080
claude mcp add --transport http pkg-go-dev http://localhost:8080/mcp
```

```json
{ "mcpServers": { "pkg-go-dev": { "type": "http", "url": "http://localhost:8080/mcp" } } }
```

A public instance is hosted on [Clever Cloud](https://www.clever.cloud) at **`https://godig.samber.dev/mcp`** — register it without running anything locally:

```sh
claude mcp add --transport http pkg-go-dev https://godig.samber.dev/mcp
```

```json
{ "mcpServers": { "pkg-go-dev": { "type": "http", "url": "https://godig.samber.dev/mcp" } } }
```

### Result cache

Because the MCP server is a long-running process and pkg.go.dev data is read-only, successful tool results are cached in memory (LRU + TTL, powered by [`samber/hot`](https://github.com/samber/hot)). Repeated tool calls for the same package are served from memory until the entry expires; errors are never cached. The cache is exclusive to the MCP server — the CLI is never cached.

```sh
# Defaults: 60-minute TTL, 100,000 entries
godig mcp
# Tune or disable (ttl=0 turns the cache off)
godig mcp --cache-ttl 30m --cache-size 50000
GODIG_CACHE_TTL=0 godig mcp
```

| Flag           | Env               | Default | Description                                |
| -------------- | ----------------- | ------- | ------------------------------------------ |
| `--cache-ttl`  | `GODIG_CACHE_TTL`  | `60m`   | Result cache TTL; `0` disables the cache.  |
| `--cache-size` | `GODIG_CACHE_SIZE` | `100000`| Result cache capacity, in entries.         |

## 🥷 Skill

A companion AI-agent skill, **`golang-pkg-go-dev`**, lives in [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang). It covers both setup (registering the MCP server) and usage workflows (intent → command/tool), and triggers when exploring Go packages: docs, versions, importers, vulnerabilities.

```sh
npx skills add https://github.com/samber/cc-skills-golang --skill golang-pkg-go-dev
```

## 🔧 How it works

- `internal/spec` is a hand-written catalog of the operations (name, flags, types).
- The CLI (`cmd/godig`) and the MCP server (`internal/mcpserver`) both build their surface by looping over that catalog — one Cobra command and one MCP tool per operation.
- `internal/dispatch` is the shared core: it maps each operation name to the matching typed [`go-pkggodev-client`](https://github.com/samber/go-pkggodev-client) call; results render as `table`, `json` or `raw`.

## 🤝 Contributing

```sh
# Install dev dependencies
make tools

# Run tests
make test

# Lint
make lint

# Build (with version ldflags)
make build
```

## 👤 Contributors

![Contributors](https://contrib.rocks/image?repo=samber/godig)

## 🙏 Acknowledgements

Thanks to [Clever Cloud](https://www.clever.cloud) for hosting the public `godig.samber.dev` MCP server.

## 💫 Show your support

Give a ⭐️ if this project helped you!

[![GitHub Sponsors](https://img.shields.io/github/sponsors/samber?style=for-the-badge)](https://github.com/sponsors/samber)

## 📝 License

Copyright © 2026 [Samuel Berthe](https://github.com/samber).

This project is [MIT](./LICENSE) licensed.
