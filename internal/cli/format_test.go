package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vnasardinov/bazelex/internal/engine"
)

// compact returns s with insignificant JSON whitespace removed, so a test can
// compare against one line.
func compact(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return buf.String()
}

func writeSQL(t *testing.T, tab *engine.Table, f Format) string {
	t.Helper()
	var b strings.Builder
	if err := WriteSQL(&b, tab, f); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestParseFormat(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{"md", Markdown, false},
		{"markdown", Markdown, false},
		{" MD ", Markdown, false},
		{"json", JSON, false},
		{"csv", CSV, false},
		{"yaml", 0, true},
		{"", 0, true},
	} {
		got, err := ParseFormat(c.in)
		if (err != nil) != c.wantErr || (!c.wantErr && got != c.want) {
			t.Errorf("ParseFormat(%q) = %v, %v; want %v, wantErr %v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestWriteSQLMarkdown(t *testing.T) {
	tab := &engine.Table{
		Columns: []string{"label", "n"},
		Rows:    [][]any{{"//p:a", int64(2)}, {"a|b", nil}},
	}
	want := "| label | n |\n" +
		"| --- | --- |\n" +
		"| //p:a | 2 |\n" +
		"| a\\|b | NULL |\n"
	if got := writeSQL(t, tab, Markdown); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteSQLMarkdownAlignsNumbers(t *testing.T) {
	tab := &engine.Table{Columns: []string{"n"}, Rows: [][]any{{int64(1)}, {int64(22)}}}
	want := "| n |\n| ---: |\n| 1 |\n| 22 |\n"
	if got := writeSQL(t, tab, Markdown); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteSQLCSV(t *testing.T) {
	tab := &engine.Table{
		Columns: []string{"label", "n"},
		Rows:    [][]any{{"//p:a", int64(2)}, {"a,b", nil}},
	}
	// encoding/csv quotes a field that contains the delimiter; NULL stays NULL.
	want := "label,n\n" +
		"//p:a,2\n" +
		"\"a,b\",NULL\n"
	if got := writeSQL(t, tab, CSV); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteSQLJSON(t *testing.T) {
	tab := &engine.Table{
		Columns: []string{"label", "n"},
		Rows:    [][]any{{"//p:a", int64(2)}, {"//p:b", nil}, {[]byte("bytes"), 1.5}},
	}
	want := `{"columns":["label","n"],"rows":[["//p:a",2],["//p:b",null],["bytes",1.5]],"row_count":3}`
	if got := compact(t, writeSQL(t, tab, JSON)); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteSQLEmptyJSON(t *testing.T) {
	tab := &engine.Table{Columns: []string{"n"}}
	want := `{"columns":["n"],"rows":[],"row_count":0}`
	if got := compact(t, writeSQL(t, tab, JSON)); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
