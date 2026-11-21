package graphdb

import (
	"bytes"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"

	bpb "github.com/bazel-contrib/buildtools/v10/build_proto"
	_ "modernc.org/sqlite"
)

func newAttr(name string, typ bpb.Attribute_Discriminator, s string, list ...string) *bpb.Attribute {
	return &bpb.Attribute{
		Name:            proto.String(name),
		Type:            typ.Enum(),
		StringValue:     proto.String(s),
		StringListValue: list,
	}
}

func explicit(a *bpb.Attribute) *bpb.Attribute {
	a.ExplicitlySpecified = proto.Bool(true)
	return a
}

func rule(name, class string, inputs []string, attrs ...*bpb.Attribute) *bpb.Target {
	return &bpb.Target{
		Type: bpb.Target_RULE.Enum(),
		Rule: &bpb.Rule{
			Name:      proto.String(name),
			RuleClass: proto.String(class),
			Attribute: attrs,
			RuleInput: inputs,
		},
	}
}

func testStream(t *testing.T) *bytes.Buffer {
	t.Helper()
	selectAttr := &bpb.Attribute{
		Name: proto.String("deps"),
		Type: bpb.Attribute_SELECTOR_LIST.Enum(),
		SelectorList: &bpb.Attribute_SelectorList{
			Type: bpb.Attribute_LABEL_LIST.Enum(),
			Elements: []*bpb.Attribute_Selector{{
				Entries: []*bpb.Attribute_SelectorEntry{
					{Label: proto.String("//conditions:linux"), StringListValue: []string{"//a:linux_only", "@ext//:lib"}},
					{Label: proto.String("//conditions:default"), IsDefaultValue: proto.Bool(true), StringListValue: []string{"//a:other"}},
				},
			}},
		},
	}
	targets := []*bpb.Target{
		rule("//a:lib", "go_library", []string{"//a:dep", "//a:hdr.go", "//tools:implicit"},
			newAttr("deps", bpb.Attribute_LABEL_LIST, "", "//a:dep", "//a:dep"),
			newAttr("srcs", bpb.Attribute_LABEL_LIST, "", "//a:hdr.go"),
			newAttr("name", bpb.Attribute_STRING, "lib"),
			explicit(newAttr("tags", bpb.Attribute_STRING_LIST, "", "manual", "no-sandbox")),
			explicit(&bpb.Attribute{Name: proto.String("testonly"), Type: bpb.Attribute_BOOLEAN.Enum(), BooleanValue: proto.Bool(true)}),
			newAttr("size", bpb.Attribute_STRING, "small"), // default, not explicit
			newAttr("visibility", bpb.Attribute_LABEL_LIST, "", "//visibility:public"),
		),
		rule("//a:dep", "go_library", nil, selectAttr),
		{
			Type:       bpb.Target_SOURCE_FILE.Enum(),
			SourceFile: &bpb.SourceFile{Name: proto.String("//a:hdr.go")},
		},
		{
			Type:          bpb.Target_GENERATED_FILE.Enum(),
			GeneratedFile: &bpb.GeneratedFile{Name: proto.String("//a:gen.go"), GeneratingRule: proto.String("//a:dep")},
		},
	}
	var buf bytes.Buffer
	for _, tg := range targets {
		if _, err := protodelim.MarshalTo(&buf, tg); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

// testDB returns an in-memory database with the schema applied.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1) // each in-memory connection is its own database
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLoad(t *testing.T) {
	db := testDB(t)

	n, err := Load(db, testStream(t))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("loaded %d targets, want 4", n)
	}

	rows, err := db.Query(`
		SELECT f.label, e.to_label, e.to_id IS NOT NULL, e.attr, e.in_select
		FROM edge e JOIN target f ON f.id = e.from_id
		ORDER BY f.label, e.attr, e.to_label`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		From, To string
		Resolved bool
		Attr     string
		InSelect int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.From, &r.To, &r.Resolved, &r.Attr, &r.InSelect); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []row{
		{"//a:dep", "//conditions:linux", false, "<select_condition>", 1},
		{"//a:dep", "//a:linux_only", false, "deps", 1},
		{"//a:dep", "//a:other", false, "deps", 1},
		{"//a:dep", "@ext//:lib", false, "deps", 1},
		{"//a:gen.go", "//a:dep", true, "<generating_rule>", 0},
		{"//a:lib", "//tools:implicit", false, "<implicit>", 0},
		{"//a:lib", "//a:dep", true, "deps", 0}, // duplicates collapsed
		{"//a:lib", "//a:hdr.go", true, "srcs", 0},
		{"//a:lib", "//visibility:public", false, "visibility", 0},
	}
	if !slices.Equal(got, want) {
		t.Errorf("edges mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestLoadAttributes(t *testing.T) {
	db := testDB(t)
	if _, err := Load(db, testStream(t)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`
		SELECT t.label, a.name, a.value
		FROM attribute a JOIN target t ON t.id = a.target_id
		ORDER BY t.label, a.name, a.value`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][3]string
	for rows.Next() {
		var r [3]string
		if err := rows.Scan(&r[0], &r[1], &r[2]); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := [][3]string{
		{"//a:lib", "tags", "manual"},
		{"//a:lib", "tags", "no-sandbox"},
		{"//a:lib", "testonly", "true"},
		{"//a:lib", "visibility", "//visibility:public"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("attributes mismatch\n got: %v\nwant: %v", got, want)
	}
}

// cycleStream returns a stream whose rules are //a:r <-> //b:r and //c:r ->
// //b:r, so packages a and b form a dependency cycle and c depends on it.
func cycleStream(t *testing.T) *bytes.Buffer {
	t.Helper()
	targets := []*bpb.Target{
		rule("//a:r", "go_library", nil, newAttr("deps", bpb.Attribute_LABEL_LIST, "", "//b:r")),
		rule("//b:r", "go_library", nil, newAttr("deps", bpb.Attribute_LABEL_LIST, "", "//a:r")),
		rule("//c:r", "go_library", nil, newAttr("deps", bpb.Attribute_LABEL_LIST, "", "//b:r")),
	}
	var buf bytes.Buffer
	for _, tg := range targets {
		if _, err := protodelim.MarshalTo(&buf, tg); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

// queryRows returns the rows of query, one string per row with the columns
// joined by spaces, so that a test can compare a whole table at once.
func queryRows(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join(vals, " "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLoadQuotients(t *testing.T) {
	db := testDB(t)
	if _, err := Load(db, cycleStream(t)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		table, query string
		want         []string
	}{
		{"qnode", `SELECT quotient, node FROM qnode ORDER BY quotient, node`,
			[]string{"package a", "package b", "package c", "scc a", "scc c"}},
		{"qedge", `SELECT quotient, from_node, to_node, weight FROM qedge ORDER BY quotient, from_node, to_node`,
			[]string{"package a b 1", "package b a 1", "package c b 1", "scc c a 1"}},
		{"qmember", `SELECT quotient, node, member FROM qmember ORDER BY quotient, node, member`,
			[]string{"scc a a", "scc a b", "scc c c"}},
	} {
		if got := queryRows(t, db, c.query); !slices.Equal(got, c.want) {
			t.Errorf("%s = %v, want %v", c.table, got, c.want)
		}
	}
}

func TestLoadQuotientLonePackage(t *testing.T) {
	// The stream's only rule-to-rule edge is within package a, so a is a node
	// of both quotients with no edge, and a component of its own.
	db := testDB(t)
	if _, err := Load(db, testStream(t)); err != nil {
		t.Fatal(err)
	}
	if got, want := queryRows(t, db, `SELECT quotient, node FROM qnode ORDER BY quotient`), []string{"package a", "scc a"}; !slices.Equal(got, want) {
		t.Errorf("qnode = %v, want %v", got, want)
	}
	if got := queryRows(t, db, `SELECT quotient, from_node, to_node FROM qedge`); len(got) != 0 {
		t.Errorf("qedge = %v, want no rows", got)
	}
	if got, want := queryRows(t, db, `SELECT quotient, node, member FROM qmember`), []string{"scc a a"}; !slices.Equal(got, want) {
		t.Errorf("qmember = %v, want %v", got, want)
	}
}

func TestSplitLabel(t *testing.T) {
	for _, tc := range []struct{ label, pkg, name string }{
		{"//a/b:c", "a/b", "c"},
		{"//:root", "", "root"},
		{"@repo//x:y", "x", "y"},
		{"//a/b", "a/b", "b"},
	} {
		pkg, name := splitLabel(tc.label)
		if pkg != tc.pkg || name != tc.name {
			t.Errorf("splitLabel(%q) = %q, %q; want %q, %q", tc.label, pkg, name, tc.pkg, tc.name)
		}
	}
}
