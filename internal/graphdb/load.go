package graphdb

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protodelim"

	// https://github.com/bazel-contrib/buildtools/blob/main/build_proto/build.proto
	bpb "github.com/bazel-contrib/buildtools/v10/build_proto"
)

const (
	attrImplicit       = "<implicit>"
	attrGeneratingRule = "<generating_rule>"
	attrSelectCond     = "<select_condition>"
)

// edge is one dependency of a target, before it is written to the database.
type edge struct {
	to       string
	attr     string
	inSelect bool
	nodep    bool
}

// attr is one non-label attribute value of a rule.
type attr struct {
	name, value string
}

// Load reads a stream of length-delimited bazel Target messages (the output of
// `bazel query --output=streamed_proto`) and writes them into db, which must
// already contain the schema. It returns the number of targets loaded.
func Load(db *sql.DB, r io.Reader) (n int, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	insTarget, err := tx.Prepare(`INSERT INTO target (label, kind, package, name, rule_class, location) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer insTarget.Close()
	insEdge, err := tx.Prepare(`INSERT INTO edge (from_id, to_label, attr, in_select, nodep) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer insEdge.Close()

	insAttr, err := tx.Prepare(`INSERT INTO attribute (target_id, name, value) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer insAttr.Close()

	br := bufio.NewReaderSize(r, 1<<20)
	for {
		t := &bpb.Target{}
		if err := protodelim.UnmarshalFrom(br, t); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return n, fmt.Errorf("reading target %d: %w", n+1, err)
		}
		label, kind, ruleClass, location, edges := describe(t)
		if label == "" {
			continue
		}
		pkg, name := splitLabel(label)
		res, err := insTarget.Exec(label, kind, pkg, name, nullable(ruleClass), nullable(location))
		if err != nil {
			return n, fmt.Errorf("inserting %s: %w", label, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return n, err
		}
		for _, e := range edges {
			if _, err := insEdge.Exec(id, e.to, e.attr, b2i(e.inSelect), b2i(e.nodep)); err != nil {
				return n, fmt.Errorf("inserting edge %s -> %s: %w", label, e.to, err)
			}
		}
		for _, a := range ruleAttrs(t.GetRule()) {
			if _, err := insAttr.Exec(id, a.name, a.value); err != nil {
				return n, fmt.Errorf("inserting attribute %s of %s: %w", a.name, label, err)
			}
		}
		n++
	}

	if _, err := tx.Exec(`UPDATE edge SET to_id = (SELECT id FROM target WHERE target.label = edge.to_label)`); err != nil {
		return n, err
	}
	return n, tx.Commit()
}

// describe extracts the interesting parts of a Target.
func describe(t *bpb.Target) (label, kind, ruleClass, location string, edges []edge) {
	switch t.GetType() {
	case bpb.Target_RULE:
		r := t.GetRule()
		return r.GetName(), "rule", r.GetRuleClass(), r.GetLocation(), ruleEdges(r)
	case bpb.Target_SOURCE_FILE:
		s := t.GetSourceFile()
		return s.GetName(), "source_file", "", s.GetLocation(), nil
	case bpb.Target_GENERATED_FILE:
		g := t.GetGeneratedFile()
		return g.GetName(), "generated_file", "", g.GetLocation(),
			[]edge{{to: g.GetGeneratingRule(), attr: attrGeneratingRule}}
	case bpb.Target_PACKAGE_GROUP:
		return t.GetPackageGroup().GetName(), "package_group", "", "", nil
	case bpb.Target_ENVIRONMENT_GROUP:
		return t.GetEnvironmentGroup().GetName(), "environment_group", "", "", nil
	}
	return "", "", "", "", nil
}

// ruleEdges returns a rule's dependencies, without duplicates. It makes two
// passes: first the edges from label-typed attributes, then an implicit edge
// for each rule input that no attribute declared.
func ruleEdges(r *bpb.Rule) []edge {
	seen := map[edge]bool{}
	var edges []edge
	add := func(e edge) {
		if e.to != "" && !seen[e] {
			seen[e] = true
			edges = append(edges, e)
		}
	}

	declared := map[string]bool{} // labels some attribute accounts for
	for _, a := range r.GetAttribute() {
		for _, e := range attributeEdges(a) {
			declared[e.to] = true
			add(e)
		}
	}
	for _, in := range r.GetRuleInput() {
		if !declared[in] {
			add(edge{to: in, attr: attrImplicit})
		}
	}
	return edges
}

// attributeEdges returns the edges from one attribute: those of its plain
// value, and those of every branch of its select(), if it has one.
func attributeEdges(a *bpb.Attribute) []edge {
	var edges []edge
	emit := func(attr string, inSelect bool, labels []string) {
		for _, l := range labels {
			edges = append(edges, edge{to: l, attr: attr, inSelect: inSelect, nodep: a.GetNodep()})
		}
	}

	emit(a.GetName(), false, labelsOf(a.GetType(), a))

	selectors := a.GetSelectorList()
	for _, sel := range selectors.GetElements() {
		for _, ent := range sel.GetEntries() {
			if !ent.GetIsDefaultValue() && ent.GetLabel() != "" {
				emit(attrSelectCond, true, []string{ent.GetLabel()})
			}
			emit(a.GetName(), true, labelsOf(selectors.GetType(), ent))
		}
	}
	return edges
}

// ruleAttrs returns the stored values of a rule's non-label attributes: those
// set explicitly in the BUILD file, plus visibility, which is always kept.
// select() values and attributes that only hold labels or outputs are skipped.
func ruleAttrs(r *bpb.Rule) []attr {
	var out []attr
	for _, a := range r.GetAttribute() {
		name := a.GetName()
		if name == "name" || (name != "visibility" && !a.GetExplicitlySpecified()) {
			continue
		}
		add := func(vs ...string) {
			for _, v := range vs {
				out = append(out, attr{name, v})
			}
		}
		switch a.GetType() {
		case bpb.Attribute_STRING:
			add(a.GetStringValue())
		case bpb.Attribute_BOOLEAN:
			add(strconv.FormatBool(a.GetBooleanValue()))
		case bpb.Attribute_INTEGER:
			add(strconv.Itoa(int(a.GetIntValue())))
		case bpb.Attribute_TRISTATE:
			add(strings.ToLower(a.GetTristateValue().String()))
		case bpb.Attribute_STRING_LIST:
			add(a.GetStringListValue()...)
		case bpb.Attribute_INTEGER_LIST:
			for _, i := range a.GetIntListValue() {
				add(strconv.Itoa(int(i)))
			}
		case bpb.Attribute_STRING_DICT:
			for _, e := range a.GetStringDictValue() {
				add(e.GetKey() + "=" + e.GetValue())
			}
		case bpb.Attribute_STRING_LIST_DICT:
			for _, e := range a.GetStringListDictValue() {
				for _, v := range e.GetValue() {
					add(e.GetKey() + "=" + v)
				}
			}
		case bpb.Attribute_LABEL_LIST:
			if name == "visibility" {
				add(a.GetStringListValue()...)
			}
		}
	}
	return out
}

// attrValue is the set of value getters that a bpb.Attribute and each entry of
// its select() have in common. Which one holds the value depends on the type.
type attrValue interface {
	GetStringValue() string
	GetStringListValue() []string
	GetLabelDictUnaryValue() []*bpb.LabelDictUnaryEntry
	GetLabelListDictValue() []*bpb.LabelListDictEntry
	GetLabelKeyedStringDictValue() []*bpb.LabelKeyedStringDictEntry
}

// labelsOf returns the labels held by v, an attribute value or select entry of
// type t, or nil if t does not hold labels.
func labelsOf(t bpb.Attribute_Discriminator, v attrValue) []string {
	switch t {
	case bpb.Attribute_LABEL:
		return []string{v.GetStringValue()}
	case bpb.Attribute_LABEL_LIST:
		return v.GetStringListValue()
	case bpb.Attribute_LABEL_DICT_UNARY:
		var out []string
		for _, e := range v.GetLabelDictUnaryValue() {
			out = append(out, e.GetValue())
		}
		return out
	case bpb.Attribute_LABEL_LIST_DICT:
		var out []string
		for _, e := range v.GetLabelListDictValue() {
			out = append(out, e.GetValue()...)
		}
		return out
	case bpb.Attribute_LABEL_KEYED_STRING_DICT:
		var out []string
		for _, e := range v.GetLabelKeyedStringDictValue() {
			out = append(out, e.GetKey())
		}
		return out
	}
	return nil
}

// splitLabel splits "@repo//pkg:name" into ("pkg", "name").
func splitLabel(label string) (pkg, name string) {
	if i := strings.Index(label, "//"); i >= 0 {
		label = label[i+2:]
	}
	if i := strings.LastIndex(label, ":"); i >= 0 {
		return label[:i], label[i+1:]
	}
	return label, label[strings.LastIndex(label, "/")+1:]
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
