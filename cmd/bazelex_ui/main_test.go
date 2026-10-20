package main

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vnasardinov/bazelex/internal/graphdb"
)

func testDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// a -> b -> c, all rules.
	stmts := []string{
		graphdb.Schema,
		`INSERT INTO target(id,label,kind,package,name) VALUES
		  (1,'//p:a','rule','p','a'),(2,'//p:b','rule','p','b'),(3,'//p:c','rule','p','c')`,
		`INSERT INTO edge VALUES (1,'//p:b',2,'deps',0,0),(2,'//p:c',3,'deps',0,0)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func get(t *testing.T, path, url string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := httptest.NewRecorder()
	newServer(db, "g.sqlite").ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	return w.Body.String()
}

func TestIndexWithoutParams(t *testing.T) {
	body := get(t, testDB(t), "/")
	for _, q := range queries {
		if !strings.Contains(body, q.Title) {
			t.Errorf("missing %q", q.Title)
		}
	}
	if strings.Contains(body, "<th>path</th>") || strings.Contains(body, "No rows") {
		t.Error("no parameterized query should run without parameters")
	}
}

func TestQueriesRun(t *testing.T) {
	path := testDB(t)
	body := get(t, path, "/?transitive-deps.target=//p:a&impact.target=//p:c&shortest-path.from=//p:a&shortest-path.to=//p:c")
	for _, want := range []string{
		"<td>//p:c</td>",                         // transitive deps of a
		"<td>//p:a</td>",                         // impact of c
		"<td>//p:a -&gt; //p:b -&gt; //p:c</td>", // path
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestSelfContainedQueries(t *testing.T) {
	path := testDB(t)
	body := get(t, path, "/")
	// Cheap queries run on load.
	if !strings.Contains(body, "<td>//p:b</td><td>1</td>") {
		t.Errorf("most-depended-on missing:\n%s", body)
	}
	if !strings.Contains(body, "<td>//p:c</td><td>2</td>") {
		t.Errorf("rule-depth missing:\n%s", body)
	}
	// The expensive one waits to be asked for.
	if !strings.Contains(body, "?run=heaviest") || strings.Contains(body, "<th>transitive_deps</th>") {
		t.Error("heaviest should be offered, not run")
	}
	body = get(t, path, "/?run=heaviest")
	if !strings.Contains(body, "<td>//p:a</td><td>2</td>") {
		t.Errorf("heaviest missing:\n%s", body)
	}
}
