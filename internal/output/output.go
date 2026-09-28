// Package output renders command results for humans (tables, key/value) or
// machines (--json). JSON output is the stable contract; human output may
// change between releases.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// preferredColumns are shown first, in this order, when rendering tables.
var preferredColumns = []string{
	"id", "slug", "name", "title", "status", "state", "type", "version",
	"campaign", "due", "due_date", "owner", "updated_at", "created_at",
}

const maxColumns = 6
const maxCell = 60

// JSON writes v as indented JSON. json.RawMessage is re-indented as-is.
func JSON(w io.Writer, v any) error {
	if raw, ok := v.(json.RawMessage); ok {
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			return err
		}
		buf.WriteByte('\n')
		_, err := w.Write(buf.Bytes())
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Human renders raw JSON in a readable form: arrays of objects as tables,
// objects as key/value lines, everything else as indented JSON.
func Human(w io.Writer, raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, err := fmt.Fprintln(w, string(raw))
		return err
	}
	// Unwrap {"items": [...]}-style envelopes with a single list field.
	if obj, ok := v.(map[string]any); ok && len(obj) == 1 {
		for _, inner := range obj {
			if arr, ok := inner.([]any); ok {
				v = arr
			}
		}
	}
	switch t := v.(type) {
	case []any:
		if rows, ok := objects(t); ok {
			return table(w, rows)
		}
	case map[string]any:
		return keyValues(w, t)
	}
	return JSON(w, raw)
}

func objects(arr []any) ([]map[string]any, bool) {
	rows := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, false
		}
		rows = append(rows, m)
	}
	return rows, true
}

func table(w io.Writer, rows []map[string]any) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(none)")
		return err
	}
	cols := columns(rows)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(cols, "\t")))
	for _, r := range rows {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = cell(r[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// columns picks up to maxColumns scalar fields: preferred names first, then
// the remaining scalar keys of the first row alphabetically.
func columns(rows []map[string]any) []string {
	seen := map[string]bool{}
	var cols []string
	add := func(k string) {
		if !seen[k] && len(cols) < maxColumns {
			seen[k] = true
			cols = append(cols, k)
		}
	}
	for _, k := range preferredColumns {
		for _, r := range rows {
			if v, ok := r[k]; ok && scalar(v) {
				add(k)
				break
			}
		}
	}
	var rest []string
	for k, v := range rows[0] {
		if !seen[k] && scalar(v) {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		add(k)
	}
	return cols
}

func keyValues(w io.Writer, obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := rank(keys[i]), rank(keys[j])
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var nested []string
	for _, k := range keys {
		if scalar(obj[k]) {
			fmt.Fprintf(tw, "%s:\t%s\n", k, text(obj[k]))
		} else {
			nested = append(nested, k)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, k := range nested {
		b, _ := json.MarshalIndent(obj[k], "  ", "  ")
		fmt.Fprintf(w, "%s:\n  %s\n", k, b)
	}
	return nil
}

func rank(k string) int {
	for i, p := range preferredColumns {
		if p == k {
			return i
		}
	}
	return len(preferredColumns)
}

func scalar(v any) bool {
	switch v.(type) {
	case nil, string, float64, bool:
		return true
	}
	return false
}

func text(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		return t
	case float64:
		return fmt.Sprintf("%g", t)
	case bool:
		return fmt.Sprintf("%t", t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func cell(v any) string {
	s := strings.ReplaceAll(text(v), "\n", " ")
	if r := []rune(s); len(r) > maxCell {
		s = string(r[:maxCell-1]) + "…"
	}
	return s
}
