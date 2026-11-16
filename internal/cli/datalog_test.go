package cli

import (
	"strings"
	"testing"

	"github.com/vnasardinov/bazelex/internal/engine"
)

func writeDatalog(t *testing.T, r *engine.DatalogResult, f Format) string {
	t.Helper()
	var b strings.Builder
	if err := WriteDatalog(&b, r, f); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// sampleResult has two relations; the second reports more rows than it shows.
func sampleResult() *engine.DatalogResult {
	return &engine.DatalogResult{EDB: 3, Derived: 2, Relations: []engine.Relation{
		{Name: "dep/2", Columns: []string{"X", "Y"}, Rows: [][]string{{"//p:a", "//p:b"}}, Total: 1},
		{Name: "reachable/2", Columns: []string{"X", "Y"}, Rows: [][]string{{"//p:a", "//p:b"}}, Total: 2},
	}}
}

func TestWriteDatalogMarkdown(t *testing.T) {
	want := "3 facts loaded, 2 derived.\n" +
		"\n## dep/2\n\n" +
		"| X | Y |\n| --- | --- |\n| //p:a | //p:b |\n" +
		"\n## reachable/2\n\n" +
		"| X | Y |\n| --- | --- |\n| //p:a | //p:b |\n" +
		"\n_Showing 1 of 2 rows._\n"
	if got := writeDatalog(t, sampleResult(), Markdown); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteDatalogCSV(t *testing.T) {
	want := "# dep/2\n" +
		"X,Y\n" +
		"//p:a,//p:b\n" +
		"\n" +
		"# reachable/2\n" +
		"X,Y\n" +
		"//p:a,//p:b\n"
	if got := writeDatalog(t, sampleResult(), CSV); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteDatalogJSON(t *testing.T) {
	want := `{"edb":3,"derived":2,"relations":[` +
		`{"name":"dep/2","columns":["X","Y"],"rows":[["//p:a","//p:b"]],"row_count":1,"total":1},` +
		`{"name":"reachable/2","columns":["X","Y"],"rows":[["//p:a","//p:b"]],"row_count":1,"total":2}]}`
	if got := compact(t, writeDatalog(t, sampleResult(), JSON)); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteDatalogEmptyJSON(t *testing.T) {
	want := `{"edb":0,"derived":0,"relations":[]}`
	if got := compact(t, writeDatalog(t, &engine.DatalogResult{}, JSON)); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLimitDatalog(t *testing.T) {
	newResult := func() *engine.DatalogResult {
		return &engine.DatalogResult{Relations: []engine.Relation{
			{Name: "x/1", Rows: [][]string{{"a"}, {"b"}, {"c"}}, Total: 3},
		}}
	}
	r := newResult()
	LimitDatalog(r, 2)
	if got := r.Relations[0]; len(got.Rows) != 2 || got.Total != 3 {
		t.Errorf("after limit 2: %d rows, total %d; want 2 rows, total 3", len(got.Rows), got.Total)
	}
	// 0 means no limit.
	r = newResult()
	LimitDatalog(r, 0)
	if len(r.Relations[0].Rows) != 3 {
		t.Errorf("limit 0 dropped rows: %v", r.Relations[0].Rows)
	}
}
