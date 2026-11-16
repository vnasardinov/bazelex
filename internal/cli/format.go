// Package cli renders the exploration engine's results as text: Markdown for
// people, JSON or CSV for other programs (such as an AI coding agent).
package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/vnasardinov/bazelex/internal/engine"
)

// Format is an output format.
type Format int

const (
	Markdown Format = iota // a table per result, for people
	JSON                   // structured, for a program
	CSV                    // comma-separated, for a program
)

func (f Format) String() string {
	switch f {
	case Markdown:
		return "md"
	case JSON:
		return "json"
	case CSV:
		return "csv"
	}
	return fmt.Sprintf("Format(%d)", int(f))
}

// ParseFormat parses a format name: md (or markdown), json or csv.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "md", "markdown":
		return Markdown, nil
	case "json":
		return JSON, nil
	case "csv":
		return CSV, nil
	}
	return 0, fmt.Errorf("unknown format %q; want md, json or csv", s)
}

// WriteSQL writes a SQL result as one table.
func WriteSQL(w io.Writer, t *engine.Table, f Format) error {
	switch f {
	case Markdown:
		rows, numeric := tableStrings(t)
		return writeMarkdownTable(w, t.Columns, rows, numeric)
	case CSV:
		rows, _ := tableStrings(t)
		return writeCSVTable(w, t.Columns, rows)
	case JSON:
		return writeJSON(w, newJSONTable(t))
	}
	return fmt.Errorf("unknown format %s", f)
}

// WriteDatalog writes a Mangle evaluation: one table per derived relation. The
// Markdown and CSV forms print the relations one after another; the JSON form
// nests them.
func WriteDatalog(w io.Writer, r *engine.DatalogResult, f Format) error {
	switch f {
	case Markdown:
		if _, err := fmt.Fprintf(w, "%d facts loaded, %d derived.\n", r.EDB, r.Derived); err != nil {
			return err
		}
		for _, rel := range r.Relations {
			if _, err := fmt.Fprintf(w, "\n## %s\n\n", rel.Name); err != nil {
				return err
			}
			if err := writeMarkdownTable(w, rel.Columns, rel.Rows, nil); err != nil {
				return err
			}
			if rel.Total > len(rel.Rows) {
				if _, err := fmt.Fprintf(w, "\n_Showing %d of %d rows._\n", len(rel.Rows), rel.Total); err != nil {
					return err
				}
			}
		}
		return nil
	case CSV:
		for i, rel := range r.Relations {
			if i > 0 {
				if _, err := io.WriteString(w, "\n"); err != nil {
					return err
				}
			}
			// A comment line names the relation; each block is plain CSV after it.
			if _, err := fmt.Fprintf(w, "# %s\n", rel.Name); err != nil {
				return err
			}
			if err := writeCSVTable(w, rel.Columns, rel.Rows); err != nil {
				return err
			}
		}
		return nil
	case JSON:
		return writeJSON(w, newJSONDatalog(r))
	}
	return fmt.Errorf("unknown format %s", f)
}

// tableStrings formats a SQL table's values as text. It also reports which
// columns hold only numbers, so a Markdown table can right-align them.
func tableStrings(t *engine.Table) (rows [][]string, numeric []bool) {
	rows = make([][]string, len(t.Rows))
	for i, r := range t.Rows {
		row := make([]string, len(r))
		for j, v := range r {
			row[j] = engine.Format(v)
		}
		rows[i] = row
	}
	numeric = make([]bool, len(t.Columns))
	for j := range numeric {
		numeric[j] = len(t.Rows) > 0
		for _, r := range t.Rows {
			numeric[j] = numeric[j] && engine.IsNumber(r[j])
		}
	}
	return rows, numeric
}

// writeMarkdownTable writes a Markdown table. numeric, if not nil, marks the
// columns to right-align; it must have one entry per column.
func writeMarkdownTable(w io.Writer, columns []string, rows [][]string, numeric []bool) error {
	var b strings.Builder
	b.WriteString("|")
	for _, c := range columns {
		fmt.Fprintf(&b, " %s |", mdCell(c))
	}
	b.WriteString("\n|")
	for i := range columns {
		if numeric != nil && numeric[i] {
			b.WriteString(" ---: |")
		} else {
			b.WriteString(" --- |")
		}
	}
	b.WriteString("\n")
	for _, row := range rows {
		b.WriteString("|")
		for _, v := range row {
			fmt.Fprintf(&b, " %s |", mdCell(v))
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// mdCell escapes a value so it is one Markdown table cell.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", "<br>")
}

// writeCSVTable writes a header row and the data rows as CSV.
func writeCSVTable(w io.Writer, columns []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(columns); err != nil {
		return err
	}
	for _, row := range rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// LimitDatalog keeps at most limit rows of each derived relation, leaving Total
// intact so the output can say what was dropped. A limit of 0 keeps everything.
func LimitDatalog(r *engine.DatalogResult, limit int) {
	if limit <= 0 {
		return
	}
	for i := range r.Relations {
		rel := &r.Relations[i]
		if len(rel.Rows) > limit {
			rel.Rows = rel.Rows[:limit]
		}
	}
}
