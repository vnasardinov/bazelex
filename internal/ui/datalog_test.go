package ui

import (
	"database/sql"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/mangle/ast"
)

// datalogPage runs program on a server over the fixture database and returns
// the rendered page.
func datalogPage(t *testing.T, program string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+testDB(t)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := httptest.NewRecorder()
	newTestServer(t, db, "g.sqlite", 20, 5).
		ServeHTTP(w, httptest.NewRequest("GET", "/datalog?program="+url.QueryEscape(program), nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	return w.Body.String()
}

// table returns the rendered table with the given caption, or "".
func table(body, caption string) string {
	i := strings.Index(body, "<caption>"+caption+"</caption>")
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], "</table>")
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

func TestDatalogReachable(t *testing.T) {
	body := datalogPage(t, datalogPrograms[0].Program)
	// dep is derived from edge, and reachable is its transitive closure.
	if got := table(body, "dep/2"); !strings.Contains(got, "<td>//p:a</td><td>//p:b</td>") {
		t.Errorf("dep table wrong:\n%s", got)
	}
	// Columns are named after the rule head's variables.
	got := table(body, "reachable/2")
	for _, want := range []string{
		"<th>X</th><th>Y</th>",
		"<td>//p:a</td><td>//p:b</td>",
		"<td>//p:b</td><td>//p:c</td>",
		"<td>//p:a</td><td>//p:c</td>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reachable table missing %q:\n%s", want, got)
		}
	}
}

func TestDatalogNegation(t *testing.T) {
	// a -> b -> c, so b and c are depended on and only a is a leaf.
	got := table(datalogPage(t, datalogPrograms[1].Program), "leaf/1")
	if got == "" {
		t.Fatal("no leaf table")
	}
	if !strings.Contains(got, "<td>//p:a</td>") || strings.Contains(got, "<td>//p:b</td>") {
		t.Errorf("leaf table wrong:\n%s", got)
	}
}

func TestDatalogPageWithoutProgram(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+testDB(t)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := httptest.NewRecorder()
	newTestServer(t, db, "g.sqlite", 20, 5).ServeHTTP(w, httptest.NewRequest("GET", "/datalog", nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	// The editor is prefilled with the first example, and the examples are links.
	if !strings.Contains(body, "<textarea") || !strings.Contains(body, "?program=") {
		t.Errorf("want an editor and example links:\n%s", body)
	}
}

func TestDatalogErrors(t *testing.T) {
	// A syntax error, and a predicate that is neither loaded nor defined.
	for _, program := range []string{"reachable(X :- .", "r(X) :- nope(X)."} {
		if body := datalogPage(t, program); !strings.Contains(body, `<p class="err">`) {
			t.Errorf("%q: no error shown:\n%s", program, body)
		}
	}
}

func TestDatalogFactLimit(t *testing.T) {
	edb := []ast.Atom{
		ast.NewAtom("target", ast.String("//p:a"), ast.String("rule"), ast.String("p")),
		ast.NewAtom("target", ast.String("//p:b"), ast.String("rule"), ast.String("p")),
	}
	// Three rules, so the cross product is nine facts, above the limit of one.
	if _, err := evalDatalog("p(X, Y) :- target(X, _, _), target(Y, _, _).", edb, 1); err == nil {
		t.Error("want a fact limit error")
	}
}
