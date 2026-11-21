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
	if !g.HasEdges("z") || g.HasEdges("nope") {
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
	if got, want := g.Deps("y"), []Dep{{To: "x", Weight: 1}, {To: "z", Weight: 1}}; !slices.Equal(got, want) {
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

func TestCollapse(t *testing.T) {
	g := New()
	g.Add("a1", "b1", 1)
	g.Add("a2", "b1", 2)
	g.Add("a2", "b2", 4)
	g.Add("b1", "a1", 8) // an edge back across the partition
	// Group by the leading letter, so a1 and a2 collapse into "a".
	q := Collapse(g, func(node string) string { return node[:1] })
	if got, want := q.Nodes(), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("Nodes() = %v, want %v", got, want)
	}
	if got, want := q.Deps("a"), []Dep{{To: "b", Weight: 7}}; !slices.Equal(got, want) {
		t.Errorf("Deps(a) = %v, want %v", got, want)
	}
	if got, want := q.Deps("b"), []Dep{{To: "a", Weight: 8}}; !slices.Equal(got, want) {
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
	if got, want := q.Deps("x"), []Dep{{To: "z", Weight: 1}}; !slices.Equal(got, want) {
		t.Errorf("Deps(x) = %v, want %v", got, want)
	}
	if got, want := q.Deps("z"), []Dep{{To: "w", Weight: 1}}; !slices.Equal(got, want) {
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
	// The names are a Partition, so Collapse takes them without a lookup.
	if name := got.Partition("y"); name != "x" {
		t.Errorf("Partition(y) = %q, want %q; y is in x's component", name, "x")
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

func TestLayers(t *testing.T) {
	// The condensation of x <-> y -> z -> w: the cycle x, then z, then w.
	q := Condense(testGraph())
	want := [][]string{{"x"}, {"z"}, {"w"}}
	if got := Layers(q); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Layers() = %v, want %v", got, want)
	}
}

func TestLayersBranches(t *testing.T) {
	// a -> b -> d and a -> c -> d: b and c share a layer, and d waits for both.
	g := New()
	g.Add("a", "b", 1)
	g.Add("a", "c", 1)
	g.Add("b", "d", 1)
	g.Add("c", "d", 1)
	want := [][]string{{"a"}, {"b", "c"}, {"d"}}
	if got := Layers(g); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Layers() = %v, want %v", got, want)
	}
}

func TestLayersIsolatedNode(t *testing.T) {
	g := New()
	g.AddNode("lonely")
	if got, want := Layers(g), [][]string{{"lonely"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Layers() = %v, want %v", got, want)
	}
}

func TestLayersCycle(t *testing.T) {
	// A cycle has no layering: everything it can't place comes back as one last
	// layer. Condense never leaves a cycle, so this is the degenerate case.
	g := New()
	g.Add("a", "b", 1)
	g.Add("b", "a", 1)
	g.Add("b", "c", 1)
	want := [][]string{{"a", "b", "c"}}
	if got := Layers(g); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Layers() = %v, want %v", got, want)
	}
}
