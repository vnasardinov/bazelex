package engine

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/vnasardinov/bazelex/internal/quotient"
)

// cycleEngine returns an engine over a fixture whose packages are a <-> b and
// c -> b: a and b form a dependency cycle, and c depends on it.
func cycleEngine(t *testing.T) *Engine {
	t.Helper()
	return fixtureEngine(t,
		`INSERT INTO target(id,label,kind,package,name) VALUES
		  (1,'//a:r','rule','a','r'),(2,'//b:r','rule','b','r'),(3,'//c:r','rule','c','r')`,
		`INSERT INTO edge VALUES (1,'//b:r',2,'deps',0,0),(2,'//a:r',1,'deps',0,0),(3,'//b:r',2,'deps',0,0)`,
	)
}

func TestPackageGraph(t *testing.T) {
	// The fixture has one package, so the quotient has no cross-package edges,
	// but the package is still a node.
	e := testEngine(t)
	g, err := e.PackageGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if g.HasEdges("p") {
		t.Error("the fixture has no cross-package edges")
	}
	if got, want := g.Nodes(), []string{"p"}; !slices.Equal(got, want) {
		t.Errorf("Nodes() = %v, want %v", got, want)
	}
}

func TestPackageGraphEdges(t *testing.T) {
	e := cycleEngine(t)
	g, err := e.PackageGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]quotient.Dep{
		"a": {{To: "b", Weight: 1}},
		"b": {{To: "a", Weight: 1}},
		"c": {{To: "b", Weight: 1}},
	}
	for pkg, deps := range want {
		if got := g.Deps(pkg); !slices.Equal(got, deps) {
			t.Errorf("Deps(%q) = %v, want %v", pkg, got, deps)
		}
	}
}

func TestCondensedGraph(t *testing.T) {
	e := cycleEngine(t)
	cond, component, err := e.CondensedGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// a and b collapse into one component named a; c is its own.
	if got, want := cond.Nodes(), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("Nodes() = %v, want %v", got, want)
	}
	if got, want := cond.Deps("c"), []quotient.Dep{{To: "a", Weight: 1}}; !slices.Equal(got, want) {
		t.Errorf("Deps(c) = %v, want %v", got, want)
	}
	if want := map[string]string{"a": "a", "b": "a", "c": "c"}; !maps.Equal(component, want) {
		t.Errorf("component = %v, want %v", component, want)
	}
}

func TestDatalogQuotientFacts(t *testing.T) {
	e := cycleEngine(t)
	res, err := e.Datalog(context.Background(), `pd(X, Y) :- qedge("package", X, Y, _).
cd(X, Y) :- qedge("scc", X, Y, _).
comp(X, Y) :- qmember("scc", X, Y).`)
	if err != nil {
		t.Fatal(err)
	}
	pd := relation(t, res, "pd/2")
	for _, want := range [][]string{{"a", "b"}, {"b", "a"}, {"c", "b"}} {
		if !slices.ContainsFunc(pd.Rows, func(r []string) bool { return slices.Equal(r, want) }) {
			t.Errorf("package edges missing %v in %v", want, pd.Rows)
		}
	}
	cd := relation(t, res, "cd/2")
	if len(cd.Rows) != 1 || !slices.Equal(cd.Rows[0], []string{"c", "a"}) {
		t.Errorf("condensed edges = %v, want just [c a]", cd.Rows)
	}
	comp := relation(t, res, "comp/2")
	for _, want := range [][]string{{"a", "a"}, {"a", "b"}, {"c", "c"}} {
		if !slices.ContainsFunc(comp.Rows, func(r []string) bool { return slices.Equal(r, want) }) {
			t.Errorf("members missing %v in %v", want, comp.Rows)
		}
	}
}
