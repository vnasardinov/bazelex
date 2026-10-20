package graphdb

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"google.golang.org/protobuf/encoding/protodelim"

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

// ruleEdges collects a rule's dependencies from its label-typed attributes,
// including every branch of select(), and then adds any rule inputs that no
// attribute accounts for (implicit dependencies).
func ruleEdges(r *bpb.Rule) []edge {
	seen := map[edge]bool{}
	var edges []edge
	add := func(e edge) {
		if e.to != "" && !seen[e] {
			seen[e] = true
			edges = append(edges, e)
		}
	}

	covered := map[string]bool{}
	for _, a := range r.GetAttribute() {
		nodep := a.GetNodep()
		emit := func(attr string, inSelect bool, labels []string) {
			for _, l := range labels {
				covered[l] = true
				add(edge{to: l, attr: attr, inSelect: inSelect, nodep: nodep})
			}
		}
		emit(a.GetName(), false, labelsOf(a.GetType(), a.GetStringValue(), a.GetStringListValue(),
			a.GetLabelDictUnaryValue(), a.GetLabelListDictValue(), a.GetLabelKeyedStringDictValue()))

		for _, sel := range a.GetSelectorList().GetElements() {
			typ := a.GetSelectorList().GetType()
			for _, ent := range sel.GetEntries() {
				if !ent.GetIsDefaultValue() && ent.GetLabel() != "" {
					emit(attrSelectCond, true, []string{ent.GetLabel()})
				}
				emit(a.GetName(), true, labelsOf(typ, ent.GetStringValue(), ent.GetStringListValue(),
					ent.GetLabelDictUnaryValue(), ent.GetLabelListDictValue(), ent.GetLabelKeyedStringDictValue()))
			}
		}
	}
	for _, in := range r.GetRuleInput() {
		if !covered[in] {
			add(edge{to: in, attr: attrImplicit})
		}
	}
	return edges
}

// labelsOf returns the labels held by an attribute value (or select entry) of
// the given type, or nil if the type does not hold labels.
func labelsOf(t bpb.Attribute_Discriminator, s string, list []string,
	unary []*bpb.LabelDictUnaryEntry, listDict []*bpb.LabelListDictEntry, keyed []*bpb.LabelKeyedStringDictEntry) []string {
	switch t {
	case bpb.Attribute_LABEL:
		return []string{s}
	case bpb.Attribute_LABEL_LIST:
		return list
	case bpb.Attribute_LABEL_DICT_UNARY:
		var out []string
		for _, e := range unary {
			out = append(out, e.GetValue())
		}
		return out
	case bpb.Attribute_LABEL_LIST_DICT:
		var out []string
		for _, e := range listDict {
			out = append(out, e.GetValue()...)
		}
		return out
	case bpb.Attribute_LABEL_KEYED_STRING_DICT:
		var out []string
		for _, e := range keyed {
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
