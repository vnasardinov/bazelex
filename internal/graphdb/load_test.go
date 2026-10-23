package graphdb

import (
	"bytes"
	"database/sql"
	"reflect"
	"testing"

	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"

	bpb "github.com/bazel-contrib/buildtools/v10/build_proto"
	_ "modernc.org/sqlite"
)

func labelAttr(name string, typ bpb.Attribute_Discriminator, s string, list ...string) *bpb.Attribute {
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
			labelAttr("deps", bpb.Attribute_LABEL_LIST, "", "//a:dep", "//a:dep"),
			labelAttr("srcs", bpb.Attribute_LABEL_LIST, "", "//a:hdr.go"),
			labelAttr("name", bpb.Attribute_STRING, "lib"),
			explicit(labelAttr("tags", bpb.Attribute_STRING_LIST, "", "manual", "no-sandbox")),
			explicit(&bpb.Attribute{Name: proto.String("testonly"), Type: bpb.Attribute_BOOLEAN.Enum(), BooleanValue: proto.Bool(true)}),
			labelAttr("size", bpb.Attribute_STRING, "small"), // default, not explicit
			labelAttr("visibility", bpb.Attribute_LABEL_LIST, "", "//visibility:public"),
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

func TestLoad(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // each in-memory connection is its own database
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}

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
	if !reflect.DeepEqual(got, want) {
		t.Errorf("edges mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestLoadAttributes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
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
	want := [][3]string{
		{"//a:lib", "tags", "manual"},
		{"//a:lib", "tags", "no-sandbox"},
		{"//a:lib", "testonly", "true"},
		{"//a:lib", "visibility", "//visibility:public"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attributes mismatch\n got: %v\nwant: %v", got, want)
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
