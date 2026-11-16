package engine

import (
	"context"
	"slices"
	"testing"

	"github.com/google/mangle/ast"
)

// relation returns the named relation of r, or fails.
func relation(t *testing.T, r *DatalogResult, name string) Relation {
	t.Helper()
	for _, rel := range r.Relations {
		if rel.Name == name {
			return rel
		}
	}
	t.Fatalf("no relation %q in %v", name, r.Relations)
	return Relation{}
}

func TestDatalogReachable(t *testing.T) {
	e := testEngine(t)
	res, err := e.Datalog(context.Background(), `dep(X, Y) :- edge(X, Y, "deps").
reachable(X, Y) :- dep(X, Y).
reachable(X, Z) :- dep(X, Y), reachable(Y, Z).`)
	if err != nil {
		t.Fatal(err)
	}
	got := relation(t, res, "reachable/2")
	// Columns are named after the rule head's variables.
	if !slices.Equal(got.Columns, []string{"X", "Y"}) {
		t.Errorf("columns = %v", got.Columns)
	}
	for _, want := range [][]string{
		{"//p:a", "//p:b"}, {"//p:b", "//p:c"}, {"//p:a", "//p:c"},
	} {
		if !slices.ContainsFunc(got.Rows, func(r []string) bool { return slices.Equal(r, want) }) {
			t.Errorf("reachable missing %v in %v", want, got.Rows)
		}
	}
	if got.Total != 3 {
		t.Errorf("total = %d, want 3", got.Total)
	}
}

func TestDatalogNegation(t *testing.T) {
	e := testEngine(t)
	res, err := e.Datalog(context.Background(), `needed(Y) :- dep(X, Y).
leaf(X) :- target(X, "rule", _), !needed(X).`)
	if err != nil {
		t.Fatal(err)
	}
	// a -> b -> c, so b and c are depended on and only a is a leaf.
	got := relation(t, res, "leaf/1")
	if len(got.Rows) != 1 || got.Rows[0][0] != "//p:a" {
		t.Errorf("leaf = %v, want just //p:a", got.Rows)
	}
}

func TestDatalogErrors(t *testing.T) {
	e := testEngine(t)
	// A syntax error, and a predicate that is neither loaded nor defined.
	for _, program := range []string{"reachable(X :- .", "r(X) :- nope(X)."} {
		if _, err := e.Datalog(context.Background(), program); err == nil {
			t.Errorf("%q: want an error", program)
		}
	}
}

func TestDatalogFactLimit(t *testing.T) {
	edb := []ast.Atom{
		ast.NewAtom("target", ast.String("//p:a"), ast.String("rule"), ast.String("p")),
		ast.NewAtom("target", ast.String("//p:b"), ast.String("rule"), ast.String("p")),
	}
	// The cross product of two targets is four facts, above the limit of one.
	if _, err := evalDatalog("p(X, Y) :- target(X, _, _), target(Y, _, _).", edb, 1); err == nil {
		t.Error("want a fact limit error")
	}
}
