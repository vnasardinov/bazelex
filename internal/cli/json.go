package cli

import (
	"encoding/json"
	"io"

	"github.com/vnasardinov/bazelex/internal/engine"
)

// Below are the shapes the JSON format emits. They are stable: a consumer can
// rely on the key names.

type jsonTable struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	RowCount int      `json:"row_count"`
}

type jsonRelation struct {
	Name     string     `json:"name"`
	Columns  []string   `json:"columns"`
	Rows     [][]string `json:"rows"`
	RowCount int        `json:"row_count"`
	Total    int        `json:"total"`
}

type jsonDatalog struct {
	EDB       int            `json:"edb"`
	Derived   int            `json:"derived"`
	Relations []jsonRelation `json:"relations"`
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newJSONTable(t *engine.Table) jsonTable {
	rows := make([][]any, len(t.Rows))
	for i, r := range t.Rows {
		row := make([]any, len(r))
		for j, v := range r {
			row[j] = jsonValue(v)
		}
		rows[i] = row
	}
	return jsonTable{Columns: t.Columns, Rows: rows, RowCount: len(t.Rows)}
}

func newJSONDatalog(r *engine.DatalogResult) jsonDatalog {
	d := jsonDatalog{EDB: r.EDB, Derived: r.Derived, Relations: make([]jsonRelation, len(r.Relations))}
	for i, rel := range r.Relations {
		// Keep an empty relation [] rather than null.
		rows := rel.Rows
		if rows == nil {
			rows = [][]string{}
		}
		d.Relations[i] = jsonRelation{
			Name:     rel.Name,
			Columns:  rel.Columns,
			Rows:     rows,
			RowCount: len(rel.Rows),
			Total:    rel.Total,
		}
	}
	return d
}

// jsonValue turns a scanned value into something encoding/json renders
// naturally: a byte slice becomes its text, not base64.
func jsonValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
