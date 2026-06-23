// Package render writes API responses to an io.Writer in json, raw, table or
// markdown form.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Write renders v to w using the given format: "json" (indented), "raw"
// (compact), "table" (human-friendly columns) or "md"/"markdown".
func Write(w io.Writer, v any, format string) error {
	// Doc/readme operations return a plain string: print it as-is for the
	// human-readable formats (it is already text/markdown).
	if s, ok := v.(string); ok {
		switch format {
		case "table", "md", "markdown":
			_, err := fmt.Fprintln(w, s)
			return err
		}
	}

	switch format {
	case "raw":
		return json.NewEncoder(w).Encode(v)
	case "json", "":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case "table":
		return writeTable(w, v)
	case "md", "markdown":
		_, err := io.WriteString(w, markdown(v))
		return err
	default:
		return fmt.Errorf("unknown output format %q (want table|json|raw|md)", format)
	}
}

// toGeneric normalises any typed value into the generic JSON shape
// (map[string]any / []any / scalar) that the table and markdown renderers
// introspect.
func toGeneric(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, err
	}
	return generic, nil
}

// writeTable normalises v through JSON and renders the most table-like view.
func writeTable(w io.Writer, v any) error {
	generic, err := toGeneric(v)
	if err != nil {
		return err
	}

	switch t := generic.(type) {
	case []any:
		return rowsTable(w, t)
	case map[string]any:
		// Prefer the first array-of-objects field (e.g. results, versions).
		for _, k := range sortedKeys(t) {
			if arr, ok := t[k].([]any); ok && len(arr) > 0 {
				if _, ok := arr[0].(map[string]any); ok {
					if _, err := fmt.Fprintf(w, "%s:\n", k); err != nil {
						return err
					}
					return rowsTable(w, arr)
				}
			}
		}
		return kvTable(w, t)
	default:
		return Write(w, v, "json")
	}
}

// rowsTable renders a slice of objects as a column table.
func rowsTable(w io.Writer, rows []any) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(no results)")
		return err
	}
	cols := unionKeys(rows)
	if len(cols) == 0 {
		// Scalar slice (e.g. import paths): one value per line.
		for _, r := range rows {
			if _, err := fmt.Fprintln(w, cell(r)); err != nil {
				return err
			}
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(cols, "\t")); err != nil {
		return err
	}
	for _, r := range rows {
		obj, _ := r.(map[string]any)
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = cell(obj[c])
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// kvTable renders a flat object as key/value rows.
func kvTable(w io.Writer, obj map[string]any) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, k := range sortedKeys(obj) {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", k, cell(obj[k])); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func unionKeys(rows []any) []string {
	seen := map[string]bool{}
	var cols []string
	for _, r := range rows {
		obj, ok := r.(map[string]any)
		if !ok {
			continue
		}
		for _, k := range sortedKeys(obj) {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	return cols
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// markdown renders v as GitHub-flavored Markdown: a table for a list of
// objects, a bullet list for a list of scalars, a key/value table for an
// object, and a fenced JSON block as a fallback.
func markdown(v any) string {
	generic, err := toGeneric(v)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	switch t := generic.(type) {
	case []any:
		mdRows(&sb, t)
	case map[string]any:
		for _, k := range sortedKeys(t) {
			if arr, ok := t[k].([]any); ok && len(arr) > 0 {
				if _, ok := arr[0].(map[string]any); ok {
					fmt.Fprintf(&sb, "## %s\n\n", k)
					mdRows(&sb, arr)
					return sb.String()
				}
			}
		}
		mdKV(&sb, t)
	default:
		jb, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintf(&sb, "```json\n%s\n```\n", jb)
	}
	return sb.String()
}

func mdRows(sb *strings.Builder, rows []any) {
	if len(rows) == 0 {
		sb.WriteString("_(no results)_\n")
		return
	}
	cols := unionKeys(rows)
	if len(cols) == 0 {
		for _, r := range rows {
			fmt.Fprintf(sb, "- %s\n", mdCell(r))
		}
		return
	}
	fmt.Fprintf(sb, "| %s |\n", strings.Join(cols, " | "))
	seps := make([]string, len(cols))
	for i := range seps {
		seps[i] = "---"
	}
	fmt.Fprintf(sb, "| %s |\n", strings.Join(seps, " | "))
	for _, r := range rows {
		obj, _ := r.(map[string]any)
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = mdCell(obj[c])
		}
		fmt.Fprintf(sb, "| %s |\n", strings.Join(cells, " | "))
	}
}

func mdKV(sb *strings.Builder, obj map[string]any) {
	sb.WriteString("| field | value |\n| --- | --- |\n")
	for _, k := range sortedKeys(obj) {
		fmt.Fprintf(sb, "| %s | %s |\n", k, mdCell(obj[k]))
	}
}

// mdCell formats a cell value safe for a Markdown table (no pipes/newlines).
func mdCell(v any) string {
	s := cell(v)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

// cell formats a scalar; nested objects/arrays become compact JSON.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
