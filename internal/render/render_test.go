package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/samber/godig/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite_JSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, render.Write(&buf, map[string]any{"path": "lo"}, "json"))
	assert.Contains(t, buf.String(), `"path": "lo"`) // indented
}

func TestWrite_Raw(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, render.Write(&buf, map[string]any{"path": "lo"}, "raw"))
	assert.Contains(t, buf.String(), `{"path":"lo"}`) // compact
}

func TestWrite_TableSlice(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rows := []map[string]any{
		{"name": "lo", "version": "v1.0.0"},
		{"name": "mo", "version": "v2.0.0"},
	}
	require.NoError(t, render.Write(&buf, rows, "table"))
	out := buf.String()
	assert.Contains(t, out, "name")
	assert.Contains(t, out, "version")
	assert.Contains(t, out, "lo")
	assert.Contains(t, out, "v2.0.0")
}

func TestWrite_TableNestedResults(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	resp := map[string]any{"results": []map[string]any{{"path": "github.com/samber/lo"}}}
	require.NoError(t, render.Write(&buf, resp, "table"))
	out := buf.String()
	assert.Contains(t, out, "results:")
	assert.Contains(t, out, "github.com/samber/lo")
}

func TestWrite_UnknownFormat(t *testing.T) {
	t.Parallel()
	err := render.Write(&strings.Builder{}, 1, "xml")
	require.Error(t, err)
}
