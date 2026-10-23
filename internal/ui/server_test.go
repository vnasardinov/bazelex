package ui

import (
	"database/sql"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

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
		// a is tagged manual; b is public but only used from its own package.
		`INSERT INTO attribute(target_id,name,value) VALUES
		  (1,'tags','manual'),(2,'visibility','//visibility:public')`,
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
	return getRows(t, path, url, 20)
}

func getRows(t *testing.T, path, url string, rows int) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := httptest.NewRecorder()
	NewServer(db, "g.sqlite", rows, 10*time.Second).ServeHTTP(w, httptest.NewRequest("GET", url, nil))
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

func TestTableOfContents(t *testing.T) {
	body := get(t, testDB(t), "/")
	nav := body[strings.Index(body, "<nav"):strings.Index(body, "</nav>")]
	for _, q := range queries {
		if want := `<a href="#` + q.ID + `">` + q.Title + `</a>`; !strings.Contains(nav, want) {
			t.Errorf("table of contents missing %q", want)
		}
	}
	if !strings.Contains(nav, `heaviest">Heaviest targets</a> <span class="note">(slow)`) {
		t.Error("expensive query not marked slow")
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
	if !strings.Contains(body, "<td>//p:b</td><td class=\"num\">1</td>") {
		t.Errorf("most-depended-on missing:\n%s", body)
	}
	// The label links to the longest-chain query.
	if !strings.Contains(body, `href="?longest-chain.target=%2F%2Fp%3Ac#longest-chain">//p:c</a></td><td class="num">2</td>`) {
		t.Errorf("rule-depth missing:\n%s", body)
	}
	// Rules tagged manual, and public rules used only in their own package.
	for _, want := range []string{
		`<section id="manual-rules">`,
		`<section id="public-package-local">`,
		"<td>//p:a</td>",
		"<td>//p:b</td>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	// b sits between a and c, so it is the only choke point. Its label links to
	// the impact query.
	if !strings.Contains(body, `href="?impact.target=%2F%2Fp%3Ab#impact">//p:b</a></td><td class="num">1</td><td class="num">1</td><td class="num">1</td>`) {
		t.Errorf("choke-points missing:\n%s", body)
	}
	// The expensive one waits to be asked for.
	if !strings.Contains(body, "?run=heaviest") || strings.Contains(body, "transitive_deps</th>") {
		t.Error("heaviest should be offered, not run")
	}
	body = get(t, path, "/?run=heaviest")
	// Numeric columns, header included, are right-aligned.
	if !strings.Contains(body, `<th class="num">transitive_deps</th>`) {
		t.Errorf("numeric header not aligned:\n%s", body)
	}
	// Its labels link to transitive-deps in a new tab, without "run", so the
	// new page doesn't re-run the expensive query.
	if !strings.Contains(body, `<a href="?transitive-deps.target=%2F%2Fp%3Aa#transitive-deps" target="_blank" rel="noopener">//p:a</a></td><td class="num">2</td>`) {
		t.Errorf("heaviest missing or links wrong:\n%s", body)
	}
}

// card fetches url and returns the transitive-deps card of the page.
func card(t *testing.T, path, url string) string {
	t.Helper()
	body := get(t, path, url)
	i := strings.Index(body, `id="transitive-deps"`)
	j := strings.Index(body[i:], "</section>")
	if i < 0 || j < 0 {
		t.Fatalf("no transitive-deps card in:\n%s", body)
	}
	return body[i : i+j]
}

func TestPagination(t *testing.T) {
	path := testDB(t)
	// Add 45 rules, each depended on by //p:a, so "impact" of nothing is
	// useless but "transitive-deps" of //p:a has 2+45 rows.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 45; i++ {
		label := fmt.Sprintf("//q:r%02d", i)
		if _, err := db.Exec(`INSERT INTO target(id,label,kind,package,name) VALUES (?,?,'rule','q','x')`, 100+i, label); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO edge VALUES (1,?,?,'deps',0,0)`, label, 100+i); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	// card returns the HTML of the transitive-deps card only; other cards
	// paginate too, given this data.
	base := "/?transitive-deps.target=//p:a&run=heaviest"
	body := card(t, path, base)
	if !strings.Contains(body, "Page 1 (rows 1&ndash;20)") || !strings.Contains(body, "Next") {
		t.Errorf("page 1 wrong:\n%s", body)
	}
	if strings.Contains(body, "Previous") {
		t.Error("page 1 should have no Previous link")
	}
	// Other parameters survive in the Next link.
	if !strings.Contains(body, "run=heaviest") || !strings.Contains(body, "transitive-deps.page=2") {
		t.Errorf("next link wrong:\n%s", body)
	}

	body = card(t, path, base+"&transitive-deps.page=3")
	if !strings.Contains(body, "Page 3 (rows 41&ndash;48)") || !strings.Contains(body, "Previous") {
		t.Errorf("last page wrong:\n%s", body)
	}
	if strings.Contains(body, "Next") {
		t.Error("last page should have no Next link")
	}

	// Past the end: no rows, but a way back.
	body = card(t, path, base+"&transitive-deps.page=9")
	if !strings.Contains(body, "No rows") || !strings.Contains(body, "Previous") {
		t.Errorf("empty page wrong:\n%s", body)
	}
}

func TestRowsFlag(t *testing.T) {
	// transitive-deps of //p:a is {//p:a, //p:b, //p:c}: three rows.
	url := "/?transitive-deps.target=//p:a"
	body := getRows(t, testDB(t), url, 2)
	if !strings.Contains(body, "rows 1&ndash;2)") || !strings.Contains(body, "transitive-deps.page=2") {
		t.Errorf("2 rows per page not honored:\n%s", body)
	}
	body = getRows(t, testDB(t), url+"&transitive-deps.page=2", 2)
	if !strings.Contains(body, "rows 3&ndash;3)") {
		t.Errorf("page 2 wrong:\n%s", body)
	}
}

func TestLongestChain(t *testing.T) {
	path := testDB(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// a -> b -> c as before, plus a shortcut a -> c and a second top-level
	// rule x -> c. The longest chain to c is still a -> b -> c.
	for _, q := range []string{
		`INSERT INTO target(id,label,kind,package,name) VALUES (4,'//p:x','rule','p','x')`,
		`INSERT INTO edge VALUES (1,'//p:c',3,'deps',0,0),(4,'//p:c',3,'deps',0,0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	got := getRows(t, path, "/?longest-chain.target=//p:c", 20)
	if !strings.Contains(got, "<td class=\"num\">2</td><td>//p:a -&gt; //p:b -&gt; //p:c</td>") {
		t.Errorf("chain wrong:\n%s", got)
	}
	// A top-level rule is its own chain, of length 0.
	got = getRows(t, path, "/?longest-chain.target=//p:x", 20)
	if !strings.Contains(got, "<td class=\"num\">0</td><td>//p:x</td>") {
		t.Errorf("root chain wrong:\n%s", got)
	}
}
