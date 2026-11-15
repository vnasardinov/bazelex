package ui

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestGraphDownloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		graphdb.Schema,
		`INSERT INTO target(id,label,kind,package,name) VALUES
		  (1,'//a/x:r','rule','a/x','r'),(2,'//b:r','rule','b','r')`,
		`INSERT INTO edge VALUES (1,'//b:r',2,'deps',0,0)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	page := get(t, path, "/graph?pkg=a/x&depth=1")
	for _, want := range []string{`href="graph.dot?depth=1&amp;pkg=a%2Fx"`, `href="graph.svg?depth=1&amp;pkg=a%2Fx"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(get(t, path, "/graph"), "Download") {
		t.Error("no package, no download links")
	}

	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	h := newTestServer(t, ro, "g.sqlite", 20, 5)
	fetch := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w
	}
	for _, c := range []struct {
		url, contentType, disposition, body string
	}{
		{"/graph.dot?pkg=a/x&depth=1", "text/vnd.graphviz; charset=utf-8", `attachment; filename=graph-a_x-depth1.dot`, "digraph"},
		{"/graph.svg?pkg=a/x&depth=1&cycle=1", "image/svg+xml", `attachment; filename=graph-a_x-depth1-cycle.svg`, "<svg"},
	} {
		w := fetch(c.url)
		if w.Code != 200 || w.Header().Get("Content-Type") != c.contentType ||
			w.Header().Get("Content-Disposition") != c.disposition || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s: status %d, %v, body %.40q", c.url, w.Code, w.Header(), w.Body.String())
		}
	}
	if w := fetch("/graph.dot?pkg=nope"); w.Code != 404 {
		t.Errorf("unknown package: status %d", w.Code)
	}
	if w := fetch("/graph.svg"); w.Code != 400 {
		t.Errorf("no package: status %d", w.Code)
	}
}

func TestDrawingCache(t *testing.T) {
	var c drawingCache
	k := func(i int) graphKey { return graphKey{pkg: strconv.Itoa(i)} }
	first := &drawing{nodes: 1}
	c.put(k(0), first)
	if c.get(k(0)) != first {
		t.Fatal("cached drawing not returned")
	}
	if c.get(graphKey{pkg: "0", depth: 1}) != nil {
		t.Error("a different key must miss")
	}
	for i := 1; i <= maxCachedDrawings; i++ {
		c.put(k(i), &drawing{})
	}
	if c.get(k(0)) != nil || c.get(k(1)) == nil || len(c.m) != maxCachedDrawings {
		t.Errorf("oldest should be evicted first; %d entries", len(c.m))
	}
	c.put(k(1), &drawing{}) // replacing a key must not evict or duplicate
	if c.get(k(2)) == nil || len(c.order) != maxCachedDrawings {
		t.Error("replacing a key changed the cache's size")
	}
}

func TestDrawingIsCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		graphdb.Schema,
		`INSERT INTO target(id,label,kind,package,name) VALUES (1,'//a:r','rule','a','r'),(2,'//b:r','rule','b','r')`,
		`INSERT INTO edge VALUES (1,'//b:r',2,'deps',0,0)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	s := &server{db: db, timeout: 10 * time.Second}
	k := graphKey{pkg: "a", depth: 1}
	d1, err := s.draw(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	// With the database gone, only the cache can answer.
	db.Close()
	d2, err := s.draw(context.Background(), k)
	if err != nil || d1 != d2 {
		t.Errorf("second draw = %p, %v; want the cached %p", d2, err, d1)
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
