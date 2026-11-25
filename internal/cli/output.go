package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/OneLiteFeatherNET/grus/internal/client"
)

const (
	formatTable = "table"
	formatJSON  = "json"
	formatYAML  = "yaml"
)

type object = client.Object

// column is one table column: a header and how to read the cell from an object.
type column struct {
	head string
	get  func(object) string
}

// field reads a (nested) key path of an object as text; missing keys are empty.
func field(path ...string) func(object) string {
	return func(o object) string { return text(dig(o, path...)) }
}

func dig(o object, path ...string) any {
	var cur any = o
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

// text renders a decoded JSON value as a short table cell.
func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, text(e))
		}
		return strings.Join(parts, ",")
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+text(t[k]))
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(t)
	}
}

// outputFormat reads the -o flag; it is validated before any command runs.
func outputFormat(cmd *cobra.Command) string {
	f := cmd.Flags().Lookup("output")
	if f == nil || f.Value.String() == "" {
		return formatTable
	}
	return f.Value.String()
}

func validFormat(f string) bool { return f == formatTable || f == formatJSON || f == formatYAML }

// printList writes items as a table, a JSON array or a YAML list.
func printList(cmd *cobra.Command, format string, items []object, cols []column) error {
	w := cmd.OutOrStdout()
	switch format {
	case formatJSON, formatYAML:
		if items == nil {
			items = []object{}
		}
		return encode(w, format, items)
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No resources found.")
		return err
	}
	return writeTable(w, items, cols)
}

// printObject writes one object; the table format falls back to YAML because it has no sensible columns.
func printObject(cmd *cobra.Command, format string, o object) error {
	if format == formatJSON {
		return encode(cmd.OutOrStdout(), formatJSON, o)
	}
	return encode(cmd.OutOrStdout(), formatYAML, o)
}

func writeTable(w io.Writer, items []object, cols []column) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.head
	}
	fmt.Fprintln(tw, strings.Join(heads, "\t"))
	for _, it := range items {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = c.get(it)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

func encode(w io.Writer, format string, v any) error {
	if format == formatJSON {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return err
		}
		_, err := w.Write(buf.Bytes())
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}
