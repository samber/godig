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
		return mapSections(w, t)
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

// mapSections renders an object that may mix scalar fields with one or more
// arrays-of-objects (e.g. `dependencies`: modulePath/version/goVersion plus
// requires/replaces/excludes). Scalar fields print first as a key/value table,
// then each object-array prints under its field name — so nothing is dropped, as
// happened when only the first array was shown. Objects with no array-of-objects
// field (e.g. `module info`, `overview`) fall back to a plain key/value table.
func mapSections(w io.Writer, obj map[string]any) error {
	scalars, sections := splitSections(obj)
	if len(sections) == 0 {
		return kvTable(w, obj)
	}
	if len(scalars) > 0 {
		if err := kvTable(w, scalars); err != nil {
			return err
		}
	}
	for _, s := range sections {
		if _, err := fmt.Fprintf(w, "\n%s:\n", s.name); err != nil {
			return err
		}
		if err := rowsTable(w, s.rows); err != nil {
			return err
		}
	}
	return nil
}

// section is a named array-of-objects field rendered as its own table.
type section struct {
	name string
	rows []any
}

// splitSections partitions an object into its scalar fields and its
// arrays-of-objects (each rendered as its own table/section), preserving the
// rows so callers need no further type assertion.
func splitSections(obj map[string]any) (scalars map[string]any, sections []section) {
	scalars = map[string]any{}
	for _, k := range sortedKeys(obj) {
		if rows, ok := objectArray(obj[k]); ok {
			sections = append(sections, section{name: k, rows: rows})
		} else {
			scalars[k] = obj[k]
		}
	}
	return scalars, sections
}

// objectArray returns v as a row slice when it is a non-empty []any whose first
// element is an object (map) — i.e. a value that renders as a table rather than a
// scalar — and reports false otherwise.
func objectArray(v any) ([]any, bool) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}
	if _, ok := arr[0].(map[string]any); !ok {
		return nil, false
	}
	return arr, true
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
		scalars, sections := splitSections(t)
		if len(sections) == 0 {
			mdKV(&sb, t)
			break
		}
		if len(scalars) > 0 {
			mdKV(&sb, scalars)
			sb.WriteString("\n")
		}
		for _, s := range sections {
			fmt.Fprintf(&sb, "## %s\n\n", s.name)
			mdRows(&sb, s.rows)
			sb.WriteString("\n")
		}
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
