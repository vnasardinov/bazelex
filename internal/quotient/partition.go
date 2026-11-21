package quotient

import "slices"

// Partition assigns each node a group name. Nodes that share a name collapse
// into one node of the quotient. A declared partition, such as package or
// repository, is a function of the node name alone; a computed one, such as the
// strongly connected components, is derived from the graph.
type Partition func(node string) string

// Collapse returns the quotient of g by part: one node per group name, and an
// edge a -> b whose weight is the sum of the weights of the edges from a node in
// a to a node in b. Edges whose ends fall in the same group are dropped. Groups
// with no cross-group edge are kept as isolated nodes, so the quotient lists
// every group.
func Collapse(g *Graph, part Partition) *Graph {
	q := New()
	for _, node := range g.Nodes() {
		from := part(node)
		q.AddNode(from)
		for _, d := range g.Deps(node) {
			if to := part(d.To); to != from {
				q.Add(from, to, d.Weight)
			}
		}
	}
	return q
}

// Condense returns the quotient of g by its strongly connected components:
// every component becomes one node, named after its smallest member. The
// result is always a DAG, so it has a topological order, which a cyclic graph
// does not.
func Condense(g *Graph) *Graph {
	return Collapse(g, ComponentIndex(g).Partition)
}

// Layers returns the nodes of g grouped into a topological layering: layer 0
// is every node nothing points at, and a node sits one layer past the deepest
// node that points at it, so every edge goes from a lower layer to a higher
// one. Each layer is sorted, and the layers are in order. Since Condense's
// result is a DAG, its layers are the levels a condensed graph is drawn in.
//
// A node in a cycle has no finite layer. On a graph that is not a DAG, the
// nodes no layering can place come back together in one last layer.
func Layers(g *Graph) [][]string {
	nodes := g.Nodes()
	// indeg counts the nodes that point at a node: the ones its layer has to
	// wait for.
	indeg := make(map[string]int, len(nodes))
	for _, n := range nodes {
		indeg[n] = 0
	}
	for _, n := range nodes {
		for _, d := range g.Deps(n) {
			indeg[d.To]++
		}
	}
	var frontier []string
	for _, n := range nodes {
		if indeg[n] == 0 {
			frontier = append(frontier, n)
		}
	}
	// Kahn's algorithm, relaxing each edge to the longest path from a layer 0
	// node. A node joins the frontier once its last predecessor has been
	// relaxed, so by then its layer is final.
	layer := map[string]int{}
	settled := map[string]bool{}
	deepest := 0
	for len(frontier) > 0 {
		var next []string
		for _, n := range frontier {
			settled[n] = true
			deepest = max(deepest, layer[n])
			for _, d := range g.Deps(n) {
				layer[d.To] = max(layer[d.To], layer[n]+1)
				indeg[d.To]--
				if indeg[d.To] == 0 {
					next = append(next, d.To)
				}
			}
		}
		frontier = next
	}
	byLayer := make(map[int][]string, deepest+1)
	for _, n := range nodes {
		if settled[n] {
			byLayer[layer[n]] = append(byLayer[layer[n]], n)
		}
	}
	layers := make([][]string, 0, deepest+1)
	for i := 0; i <= deepest; i++ {
		if l := byLayer[i]; len(l) > 0 {
			slices.Sort(l)
			layers = append(layers, l)
		}
	}
	var rest []string
	for _, n := range nodes {
		if !settled[n] {
			rest = append(rest, n)
		}
	}
	if len(rest) > 0 {
		layers = append(layers, rest)
	}
	return layers
}
