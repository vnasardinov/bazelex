package engine

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/vnasardinov/bazelex/internal/graphdb"
)

// fixtureEngine returns an engine over a database built from stmts, which are
// executed after the schema.
func fixtureEngine(t *testing.T, stmts ...string) *Engine {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range append([]string{graphdb.Schema}, stmts...) {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close() })
	e, err := Open(ro, Config{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// testEngine returns an engine over a fixture with a -> b -> c, all rules in
// package p; a is tagged manual and b is public.
func testEngine(t *testing.T) *Engine {
	t.Helper()
	return fixtureEngine(t,
		`INSERT INTO target(id,label,kind,package,name) VALUES
		  (1,'//p:a','rule','p','a'),(2,'//p:b','rule','p','b'),(3,'//p:c','rule','p','c')`,
		`INSERT INTO edge VALUES (1,'//p:b',2,'deps',0,0),(2,'//p:c',3,'deps',0,0)`,
		`INSERT INTO attribute(target_id,name,value) VALUES
		  (1,'tags','manual'),(2,'visibility','//visibility:public')`,
	)
}

func TestQuery(t *testing.T) {
	e := testEngine(t)
	tab, err := e.Query(context.Background(), `SELECT label, kind FROM target ORDER BY label`)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tab.Columns, []string{"label", "kind"}) {
		t.Errorf("columns = %v", tab.Columns)
	}
	want := [][]any{{"//p:a", "rule"}, {"//p:b", "rule"}, {"//p:c", "rule"}}
	if !slices.EqualFunc(tab.Rows, want, func(a, b []any) bool { return slices.Equal(a, b) }) {
		t.Errorf("rows = %v", tab.Rows)
	}
}

func TestQueryKeepsTypes(t *testing.T) {
	e := testEngine(t)
	// A number, a NULL and text, to check the scanning keeps them apart.
	tab, err := e.Query(context.Background(), `SELECT COUNT(*) AS n, NULL AS x, 'z' AS s FROM target`)
	if err != nil {
		t.Fatal(err)
	}
	if len(tab.Rows) != 1 {
		t.Fatalf("rows = %v", tab.Rows)
	}
	row := tab.Rows[0]
	if n, ok := row[0].(int64); !ok || n != 3 {
		t.Errorf("count = %#v, want int64(3)", row[0])
	}
	if row[1] != nil {
		t.Errorf("NULL = %#v, want nil", row[1])
	}
	if s, ok := row[2].(string); !ok || s != "z" {
		t.Errorf("text = %#v, want \"z\"", row[2])
	}
}

func TestQueryPage(t *testing.T) {
	e := testEngine(t)
	const q = `SELECT label FROM target ORDER BY label`
	tab, hasMore, err := e.QueryPage(context.Background(), 0, 2, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(tab.Rows) != 2 || !hasMore {
		t.Errorf("first page = %v, hasMore %v", tab.Rows, hasMore)
	}
	tab, hasMore, err = e.QueryPage(context.Background(), 2, 2, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(tab.Rows) != 1 || hasMore {
		t.Errorf("second page = %v, hasMore %v", tab.Rows, hasMore)
	}
}

func TestRuleLabels(t *testing.T) {
	e := testEngine(t)
	labels, err := e.RuleLabels(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"//p:a", "//p:b", "//p:c"}; !slices.Equal(labels, want) {
		t.Errorf("labels = %v, want %v", labels, want)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := (Config{Timeout: time.Second, MaxFacts: 10}).Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	// Zero values fall back to the defaults and are accepted.
	if err := (Config{}).Validate(); err != nil {
		t.Errorf("zero config rejected: %v", err)
	}
	if err := (Config{Timeout: -time.Second}).Validate(); err == nil {
		t.Error("negative timeout accepted")
	}
	if err := (Config{MaxFacts: -1}).Validate(); err == nil {
		t.Error("negative max facts accepted")
	}
}

func TestFormat(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{[]byte("bytes"), "bytes"},
		{int64(3), "3"},
		{float64(1.5), "1.5"},
		{"text", "text"},
	} {
		if got := Format(c.in); got != c.want {
			t.Errorf("Format(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
	if !IsNumber(int64(1)) || !IsNumber(float64(1)) || IsNumber("1") || IsNumber(nil) {
		t.Error("IsNumber misclassified a value")
	}
}
