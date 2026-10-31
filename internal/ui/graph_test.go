package ui

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vnasardinov/bazelex/internal/graphdb"
)

// testPkgGraph is x <-> y -> z -> w.
func testPkgGraph() *pkgGraph {
	g := newPkgGraph()
	g.add("x", "y", 2)
	g.add("y", "x", 1)
	g.add("y", "z", 1)
	g.add("z", "w", 1)
	return g
}

func TestComponent(t *testing.T) {
	g := testPkgGraph()
	if got := g.component("y"); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("component(y) = %v", got)
	}
	if got := g.component("z"); !slices.Equal(got, []string{"z"}) {
		t.Errorf("component(z) = %v", got)
	}
}

func TestNeighborhood(t *testing.T) {
	g := testPkgGraph()
	for _, c := range []struct {
		centers []string
		depth   int
		limit   int
		want    []string
		trunc   bool
	}{
		{[]string{"z"}, 0, 10, []string{"z"}, false},
		{[]string{"z"}, 1, 10, []string{"z", "w", "y"}, false},
		{[]string{"z"}, 2, 10, []string{"z", "w", "y", "x"}, false},
		{[]string{"x", "y"}, 1, 10, []string{"x", "y", "z"}, false},
		{[]string{"z"}, 5, 2, []string{"z", "w"}, true},
	} {
		got, trunc := g.neighborhood(c.centers, c.depth, c.limit)
		if !slices.Equal(got, c.want) || trunc != c.trunc {
			t.Errorf("neighborhood(%v, %d, %d) = %v, %v; want %v, %v", c.centers, c.depth, c.limit, got, trunc, c.want, c.trunc)
		}
	}
}

func TestDOTQuote(t *testing.T) {
	if got := dotQuote(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("got %s", got)
	}
}

func TestGraphPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Packages a and b depend on each other; b also depends on c.
	for _, s := range []string{
		graphdb.Schema,
		`INSERT INTO target(id,label,kind,package,name) VALUES
		  (1,'//a:r','rule','a','r'),(2,'//b:r','rule','b','r'),(3,'//c:r','rule','c','r')`,
		`INSERT INTO edge VALUES (1,'//b:r',2,'deps',0,0),(2,'//a:r',1,'deps',0,0),(2,'//c:r',3,'deps',0,0)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	body := get(t, path, "/graph?pkg=a&cycle=1&depth=1")
	for _, want := range []string{"<svg", "graph?cycle=1&amp;depth=1&amp;pkg=c", "dependency cycle of 2 packages"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if !strings.Contains(get(t, path, "/graph?pkg=nope"), "no dependencies") {
		t.Error("unknown package should be reported")
	}
	if strings.Contains(get(t, path, "/graph"), "<svg") {
		t.Error("no package, no drawing")
	}
}

func TestCycleQueryLinksToGraph(t *testing.T) {
	var v queryView
	v.GraphColumn = "comp"
	v.Columns = []string{"comp", "packages"}
	v.Rows = [][]cell{{{Text: "a/b"}, {Text: "2"}}}
	v.addRowLinks(nil)
	if got := v.Rows[0][0].URL; got != "graph?cycle=1&depth=1&pkg=a%2Fb" || !v.Rows[0][0].NewTab {
		t.Errorf("link = %q", got)
	}
}
