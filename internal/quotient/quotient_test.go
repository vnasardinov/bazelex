package quotient

import (
	"slices"
	"testing"
)

// testGraph is x <-> y -> z -> w.
func testGraph() *Graph {
	g := New()
	g.Add("x", "y", 2)
	g.Add("y", "x", 1)
	g.Add("y", "z", 1)
	g.Add("z", "w", 1)
	return g
}

func TestAddAndWeight(t *testing.T) {
	g := testGraph()
	if got := g.Weight("x", "y"); got != 2 {
		t.Errorf("Weight(x, y) = %d, want 2", got)
	}
	g.Add("x", "y", 3) // parallel edges sum
	if got := g.Weight("x", "y"); got != 5 {
		t.Errorf("Weight after a second edge = %d, want 5", got)
	}
	g.Add("x", "y", 0) // a non-positive weight is ignored
	if got := g.Weight("x", "y"); got != 5 {
		t.Errorf("Weight after a zero edge = %d, want 5", got)
	}
	if got := g.Weight("y", "w"); got != 0 {
		t.Errorf("Weight of a missing edge = %d, want 0", got)
	}
	if !g.Has("z") || g.Has("nope") {
		t.Error("Has misclassified a node")
	}
}

func TestNodes(t *testing.T) {
	g := New()
	g.AddNode("isolated")
	g.Add("b", "a", 1)
	if got, want := g.Nodes(), []string{"a", "b", "isolated"}; !slices.Equal(got, want) {
		t.Errorf("Nodes() = %v, want %v", got, want)
	}
}

func TestDeps(t *testing.T) {
	g := testGraph()
	if got, want := g.Deps("y"), []Dep{{To: "x", N: 1}, {To: "z", N: 1}}; !slices.Equal(got, want) {
		t.Errorf("Deps(y) = %v, want %v", got, want)
	}
	if got := g.Deps("w"); got != nil {
		t.Errorf("Deps(w) = %v, want nil", got)
	}
}

func TestComponent(t *testing.T) {
	g := testGraph()
	if got := g.Component("y"); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("Component(y) = %v", got)
	}
	if got := g.Component("z"); !slices.Equal(got, []string{"z"}) {
		t.Errorf("Component(z) = %v", got)
	}
}

func TestNeighborhood(t *testing.T) {
	g := testGraph()
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

func TestOf(t *testing.T) {
	g := New()
	g.Add("a1", "b1", 1)
	g.Add("a2", "b1", 2)
	g.Add("a2", "b2", 4)
	g.Add("b1", "a1", 8) // an edge back across the partition
	// Group by the leading letter, so a1 and a2 collapse into "a".
	q := Of(g, func(node string) string { return node[:1] })
	if got, want := q.Nodes(), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("Nodes() = %v, want %v", got, want)
	}
	if got, want := q.Deps("a"), []Dep{{To: "b", N: 7}}; !slices.Equal(got, want) {
		t.Errorf("Deps(a) = %v, want %v", got, want)
	}
	if got, want := q.Deps("b"), []Dep{{To: "a", N: 8}}; !slices.Equal(got, want) {
		t.Errorf("Deps(b) = %v, want %v", got, want)
	}
}

func TestCondense(t *testing.T) {
	g := testGraph()
	q := Condense(g)
	// x and y are one component; z and w are their own.
	if got, want := q.Nodes(), []string{"w", "x", "z"}; !slices.Equal(got, want) {
		t.Fatalf("Nodes() = %v, want %v", got, want)
	}
	if got, want := q.Deps("x"), []Dep{{To: "z", N: 1}}; !slices.Equal(got, want) {
		t.Errorf("Deps(x) = %v, want %v", got, want)
	}
	if got, want := q.Deps("z"), []Dep{{To: "w", N: 1}}; !slices.Equal(got, want) {
		t.Errorf("Deps(z) = %v, want %v", got, want)
	}
	// The condensation must be acyclic: no node reaches itself.
	for _, n := range q.Nodes() {
		if len(q.Component(n)) != 1 {
			t.Errorf("component of %q has %d nodes, want 1 (not a DAG)", n, len(q.Component(n)))
		}
	}
}

func TestComponentIndex(t *testing.T) {
	g := testGraph()
	want := map[string]string{"x": "x", "y": "x", "z": "z", "w": "w"}
	got := ComponentIndex(g)
	if len(got) != len(want) {
		t.Fatalf("ComponentIndex returned %d entries, want %d", len(got), len(want))
	}
	for node, name := range want {
		if got[node] != name {
			t.Errorf("ComponentIndex[%q] = %q, want %q", node, got[node], name)
		}
	}
}

func TestComponents(t *testing.T) {
	g := testGraph()
	want := [][]string{{"w"}, {"x", "y"}, {"z"}}
	if got := Components(g); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Components() = %v, want %v", got, want)
	}
}

func TestComponentIndexIsolatedNode(t *testing.T) {
	g := New()
	g.AddNode("lonely")
	// An isolated node is its own component, named after itself.
	if got := ComponentIndex(g); got["lonely"] != "lonely" {
		t.Errorf("ComponentIndex[lonely] = %q, want %q", got["lonely"], "lonely")
	}
	if got, want := Components(g), [][]string{{"lonely"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Components() = %v, want %v", got, want)
	}
}
