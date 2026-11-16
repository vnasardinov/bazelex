package engine

import (
	"context"
	"slices"
	"testing"
)

// testPkgGraph is x <-> y -> z -> w.
func testPkgGraph() *PkgGraph {
	g := newPkgGraph()
	g.add("x", "y", 2)
	g.add("y", "x", 1)
	g.add("y", "z", 1)
	g.add("z", "w", 1)
	return g
}

func TestComponent(t *testing.T) {
	g := testPkgGraph()
	if got := g.Component("y"); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("Component(y) = %v", got)
	}
	if got := g.Component("z"); !slices.Equal(got, []string{"z"}) {
		t.Errorf("Component(z) = %v", got)
	}
}

func TestNeighborhood(t *testing.T) {
	g := testPkgGraph()
	for _, c := range []struct {
		centers []string
		depth   int
		limit   int
		want    []string
		trunc   bool
	}{
		{[]string{"z"}, 0, 10, []string{"z"}, false},
		{[]string{"z"}, 1, 10, []string{"z", "w", "y"}, false},
		{[]string{"z"}, 2, 10, []string{"z", "w", "y", "x"}, false},
		{[]string{"x", "y"}, 1, 10, []string{"x", "y", "z"}, false},
		{[]string{"z"}, 5, 2, []string{"z", "w"}, true},
	} {
		got, trunc := g.Neighborhood(c.centers, c.depth, c.limit)
		if !slices.Equal(got, c.want) || trunc != c.trunc {
			t.Errorf("Neighborhood(%v, %d, %d) = %v, %v; want %v, %v", c.centers, c.depth, c.limit, got, trunc, c.want, c.trunc)
		}
	}
}

func TestDeps(t *testing.T) {
	g := testPkgGraph()
	want := []Dep{{To: "x", N: 1}, {To: "z", N: 1}}
	if got := g.Deps("y"); !slices.Equal(got, want) {
		t.Errorf("Deps(y) = %v, want %v", got, want)
	}
}

func TestHas(t *testing.T) {
	g := testPkgGraph()
	if !g.Has("x") || g.Has("nope") {
		t.Error("Has misclassified a package")
	}
}

func TestPackageGraph(t *testing.T) {
	// The fixture has one package, so the quotient has no cross-package edges.
	e := testEngine(t)
	g, err := e.PackageGraph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if g.Has("p") {
		t.Error("the fixture has no cross-package edges")
	}
}
