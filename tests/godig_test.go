// Package tests drives the compiled godig binary end-to-end against a fake
// pkg.go.dev server. Run via `make test` (with the unit tests) or `make
// integration` (this suite only).
package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samber/godig/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var godigBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "godig")
	if err != nil {
		panic(err)
	}
	godigBin = filepath.Join(dir, "godig")

	build := exec.Command("go", "build", "-o", godigBin, "./cmd/godig")
	build.Dir = ".." // repo root
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		_ = os.RemoveAll(dir)
		panic("build failed: " + err.Error())
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeAPI returns an httptest server emulating the pkg.go.dev endpoints godig uses.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(p, "/package/") && strings.Contains(p, "notfound"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(p, "/v1beta/package/"):
			_, _ = w.Write([]byte(`{"path":"github.com/samber/lo","name":"lo","synopsis":"Lodash for Go","isLatest":true}`))
		case strings.HasPrefix(p, "/v1beta/versions/"):
			_, _ = w.Write([]byte(`{"items":[{"version":"v1.0.0","modulePath":"github.com/samber/lo","commitTime":"2026-03-02T15:10:24Z"}],"total":1}`))
		case strings.HasPrefix(p, "/v1beta/imported-by/"):
			_, _ = w.Write([]byte(`{"modulePath":"github.com/samber/lo","importedBy":{"items":["example.com/a","example.com/b"],"total":2}}`))
		// Vulnerabilities are sourced from the Go vuln database (vuln.go.dev), served
		// as static JSON: a triage index plus per-ID OSV reports. An empty index
		// means no module has vulns, so `vulns` resolves to an empty result.
		case p == "/index/modules.json":
			_, _ = w.Write([]byte(`[]`))
		case strings.HasPrefix(p, "/ID/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type result struct {
	stdout string
	stderr string
	code   int
}

func run(t *testing.T, baseURL, stdin string, args ...string) result {
	t.Helper()
	full := []string{"--log-level", "off"}
	if baseURL != "" {
		full = append(full, "--base-url", baseURL+"/v1beta")
		// vuln.go.dev endpoints live at the server root (/index/..., /ID/...).
		full = append(full, "--vuln-base-url", baseURL)
	}
	full = append(full, args...)

	cmd := exec.Command(godigBin, full...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !asExit(err, &ee) {
			t.Fatalf("run failed: %v (stderr: %s)", err, errb.String())
		}
		code = ee.ExitCode()
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

func asExit(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

func TestOverview_ComposesCompactSummary(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "overview", "github.com/samber/lo", "-o", "json")
	require.Equal(t, 0, res.code, res.stderr)

	var ov struct {
		Name           string   `json:"name"`
		RecentVersions []string `json:"recentVersions"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &ov))
	assert.Equal(t, "lo", ov.Name)
	assert.Contains(t, ov.RecentVersions, "v1.0.0")
}

func TestSearch_RequiresQueryArg(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "search") // missing <query>
	assert.Equal(t, 2, res.code)         // usage error
	assert.Contains(t, res.stdout, "Usage:")
	assert.Contains(t, res.stdout, "godig search <query>")
}

func TestVersions_JSON(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "versions", "github.com/samber/lo", "-o", "json")
	require.Equal(t, 0, res.code, res.stderr)

	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "v1.0.0", items[0]["version"])
}

func TestPackage_Table(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "package", "info", "github.com/samber/lo", "-o", "table")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "path")
	assert.Contains(t, res.stdout, "github.com/samber/lo")
	assert.NotContains(t, res.stdout, "{") // no JSON blob in a table cell
}

func TestImportedBy_TableHasColumn(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "imported-by", "github.com/samber/lo", "-o", "table")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "package")       // header
	assert.Contains(t, res.stdout, "example.com/a") // row
	assert.NotContains(t, res.stdout, "nextToken")  // no cursor
}

func TestVulns_NullItemsIsEmpty(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "vulns", "github.com/samber/lo", "-o", "json")
	require.Equal(t, 0, res.code, res.stderr)

	var items []any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &items))
	assert.Empty(t, items)
}

// TestVulns_PopulatesSummaryAndRanges exercises the v0.5.0 data source
// (vuln.go.dev / OSV): summary, details and per-range fix versions must now be
// filled — the whole point of the upgrade. The old /v1beta/vulns endpoint left
// summary and fixedVersion empty.
func TestVulns_PopulatesSummaryAndRanges(t *testing.T) {
	t.Parallel()
	const modulePath = "github.com/example/vuln"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/index/modules.json":
			_, _ = w.Write([]byte(`[{"path":"github.com/example/vuln","vulns":[{"id":"GO-2020-0001","modified":"2020-01-01T00:00:00Z","fixed":"1.2.3"}]}]`))
		case "/ID/GO-2020-0001.json":
			_, _ = w.Write([]byte(`{
				"id":"GO-2020-0001",
				"summary":"Authorization bypass in github.com/example/vuln",
				"details":"A detailed description of the flaw.",
				"aliases":["CVE-2020-0001"],
				"affected":[{
					"package":{"name":"github.com/example/vuln","ecosystem":"Go"},
					"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.2.3"}]}],
					"ecosystem_specific":{"imports":[{"path":"github.com/example/vuln"}]}
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res := run(t, srv.URL, "", "vulns", modulePath, "-o", "json")
	require.Equal(t, 0, res.code, res.stderr)

	var items []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
		Ranges  []struct {
			Fixed string `json:"fixed"`
		} `json:"ranges"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "GO-2020-0001", items[0].ID)
	assert.Equal(t, "Authorization bypass in github.com/example/vuln", items[0].Summary)
	require.Len(t, items[0].Ranges, 1)
	assert.Equal(t, "1.2.3", items[0].Ranges[0].Fixed)
}

func TestNotFound_FriendlyError(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "package", "info", "github.com/notfound/x")
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.stderr, "not found:")
	assert.NotContains(t, res.stderr, "status code") // raw ogen error hidden
}

func TestInvalidArgs_ShowsHelp(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "imported-by") // missing <path>
	assert.Equal(t, 2, res.code)              // usage error
	assert.Contains(t, res.stdout, "Usage:")
	assert.Contains(t, res.stdout, "godig imported-by <path>")
}

func TestGroupWithoutSubcommand_IsUsageError(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "package") // group, no subcommand
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stdout, "Usage:")
}

func TestNonPositiveLimit_IsError(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	res := run(t, srv.URL, "", "versions", "github.com/samber/lo", "--limit", "0")
	assert.Equal(t, 2, res.code) // usage error
	assert.Contains(t, res.stderr, "limit must be a positive integer")
}

func TestMCP_Stdio_ToolsList(t *testing.T) {
	t.Parallel()
	srv := fakeAPI(t)
	stdin := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"godig","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	res := run(t, srv.URL, stdin, "mcp")
	require.Equal(t, 0, res.code, res.stderr)

	var tools int
	for line := range strings.SplitSeq(res.stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil && msg.ID == 2 {
			tools = len(msg.Result.Tools)
		}
	}
	// One MCP tool per spec operation (groups add no tool of their own).
	assert.Equal(t, len(spec.Operations), tools)
}
