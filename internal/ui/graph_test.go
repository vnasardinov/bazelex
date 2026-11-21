package ui

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vnasardinov/bazelex/internal/engine"
	"github.com/vnasardinov/bazelex/internal/graphdb"
)

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
		// The quotient tables bazelex writes: a and b form a cycle, named a,
		// and b depends on c.
		`INSERT INTO qnode(quotient,node) VALUES
		  ('package','a'),('package','b'),('package','c'),('scc','a'),('scc','c')`,
		`INSERT INTO qedge(quotient,from_node,to_node,weight) VALUES
		  ('package','a','b',1),('package','b','a',1),('package','b','c',1),('scc','a','c',1)`,
		`INSERT INTO qmember(quotient,node,member) VALUES
		  ('scc','a','a'),('scc','a','b'),('scc','c','c')`,
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
		// The quotient tables bazelex writes: a/x depends on b, no cycle.
		`INSERT INTO qnode(quotient,node) VALUES
		  ('package','a/x'),('package','b'),('scc','a/x'),('scc','b')`,
		`INSERT INTO qedge(quotient,from_node,to_node,weight) VALUES
		  ('package','a/x','b',1),('scc','a/x','b',1)`,
		`INSERT INTO qmember(quotient,node,member) VALUES ('scc','a/x','a/x'),('scc','b','b')`,
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
		// The component graph is layered, so its DOT groups nodes by rank.
		{"/graph.dot?by=scc&pkg=a/x&depth=1", "text/vnd.graphviz; charset=utf-8", `attachment; filename=graph-scc-a_x-depth1.dot`, "{ rank=same;"},
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

func TestGraphComponentPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Packages a and b form a cycle (component a), which depends on c.
	for _, s := range []string{
		graphdb.Schema,
		`INSERT INTO target(id,label,kind,package,name) VALUES
		  (1,'//a:r','rule','a','r'),(2,'//b:r','rule','b','r'),(3,'//c:r','rule','c','r')`,
		`INSERT INTO edge VALUES (1,'//b:r',2,'deps',0,0),(2,'//a:r',1,'deps',0,0),(2,'//c:r',3,'deps',0,0)`,
		// The quotient tables bazelex writes: a and b form a cycle, named a,
		// and b depends on c.
		`INSERT INTO qnode(quotient,node) VALUES
		  ('package','a'),('package','b'),('package','c'),('scc','a'),('scc','c')`,
		`INSERT INTO qedge(quotient,from_node,to_node,weight) VALUES
		  ('package','a','b',1),('package','b','a',1),('package','b','c',1),('scc','a','c',1)`,
		`INSERT INTO qmember(quotient,node,member) VALUES
		  ('scc','a','a'),('scc','a','b'),('scc','c','c')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	// b is in the cycle named a, so the page centers on that component.
	body := get(t, path, "/graph?by=scc&pkg=b&depth=1")
	for _, want := range []string{
		"<svg",
		"Component graph",
		"2 components; <code>b</code> is in a dependency cycle of 2 packages.",
		"graph?cycle=1&amp;depth=1&amp;pkg=b", // the same view over the package graph
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	// The DOT names the cycle by size, layers it, and links on to its dependency.
	dot := get(t, path, "/graph.dot?by=scc&pkg=b&depth=1")
	for _, want := range []string{`label="a (2)"`, "{ rank=same;", "graph?by=scc&depth=1&pkg=c"} {
		if !strings.Contains(dot, want) {
			t.Errorf("DOT missing %q:\n%s", want, dot)
		}
	}
	if !strings.Contains(get(t, path, "/graph?by=scc&pkg=nope"), "no dependencies") {
		t.Error("unknown package should be reported")
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
		// The quotient tables bazelex writes: a depends on b, no cycle.
		`INSERT INTO qnode(quotient,node) VALUES ('package','a'),('package','b'),('scc','a'),('scc','b')`,
		`INSERT INTO qedge(quotient,from_node,to_node,weight) VALUES ('package','a','b',1),('scc','a','b',1)`,
		`INSERT INTO qmember(quotient,node,member) VALUES ('scc','a','a'),('scc','b','b')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	eng, err := engine.Open(db, engine.Config{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	s := &server{eng: eng}
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
	v.addLinks(nil)
	if got := v.Rows[0][0].URL; got != "graph?cycle=1&depth=1&pkg=a%2Fb" || !v.Rows[0][0].NewTab {
		t.Errorf("link = %q", got)
	}
}
