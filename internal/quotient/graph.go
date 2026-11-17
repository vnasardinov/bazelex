// Package quotient builds quotient graphs of the build graph. A quotient
// collapses a set of nodes into one, so that a large graph can be seen at the
// granularity a question is asked at: by package, by ownership, by graph
// structure, and so on.
//
// The building blocks are Graph, a weighted directed graph, and Partition, a
// map from a node to the group it collapses into. Of applies a partition, and
// Condense applies the one derived from the graph's strongly connected
// components, which turns any graph into a DAG.
package quotient

import (
	"slices"
)

// Graph is a weighted directed graph whose nodes are strings. Weights are
// positive counts; Add sums them, so parallel edges collapse into one. The
// zero value is not usable: call New.
type Graph struct {
	out   map[string]map[string]int
	in    map[string]map[string]int
	nodes map[string]bool
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		out:   map[string]map[string]int{},
		in:    map[string]map[string]int{},
		nodes: map[string]bool{},
	}
}

// AddNode records node, so that it appears in Nodes even if it has no edge. It
// is idempotent.
func (g *Graph) AddNode(node string) {
	g.nodes[node] = true
}

// Add adds weight to the edge from -> to, recording both endpoints. A
// non-positive weight is ignored.
func (g *Graph) Add(from, to string, weight int) {
	if weight <= 0 {
		return
	}
	g.nodes[from] = true
	g.nodes[to] = true
	addDirected(g.out, from, to, weight)
	addDirected(g.in, to, from, weight)
}

// addDirected adds weight to the edge from -> to of one adjacency map: g.out
// for a forward edge, g.in for a backward one. Splitting the two lets Add name
// the direction it records instead of swapping a tuple of maps and names.
func addDirected(adj map[string]map[string]int, from, to string, weight int) {
	if adj[from] == nil {
		adj[from] = map[string]int{}
	}
	adj[from][to] += weight
}

// HasEdges reports whether node is an endpoint of an edge, in either direction.
// A node with no edge, such as one added by AddNode alone, reports false; use
// Nodes for membership.
func (g *Graph) HasEdges(node string) bool {
	_, o := g.out[node]
	_, i := g.in[node]
	return o || i
}

// Nodes returns every node, whether or not it has an edge, in name order.
func (g *Graph) Nodes() []string {
	nodes := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		nodes = append(nodes, n)
	}
	slices.Sort(nodes)
	return nodes
}

// Dep is one outgoing edge: the node it points at and the edge's weight.
type Dep struct {
	To     string
	Weight int
}

// Deps returns the nodes from points at, in name order, or nil if it points at
// none. It is successors with the edge weights attached.
func (g *Graph) Deps(from string) []Dep {
	succ := g.successors(from)
	if len(succ) == 0 {
		return nil
	}
	deps := make([]Dep, 0, len(succ))
	for _, to := range succ {
		deps = append(deps, Dep{To: to, Weight: g.out[from][to]})
	}
	return deps
}

// Weight returns the weight of the edge from -> to, or 0 if there is none.
func (g *Graph) Weight(from, to string) int {
	return g.out[from][to]
}

// adjacencies returns the graph's adjacency maps, forward (out) then backward
// (in): the directions a walk that ignores edge direction ranges over.
func (g *Graph) adjacencies() [2]map[string]map[string]int {
	return [2]map[string]map[string]int{g.out, g.in}
}

// successors returns the names from points at, in name order. The SCC pass walks
// it, so it is kept unexported and allocation-light; Deps is this list with the
// weights attached.
func (g *Graph) successors(from string) []string {
	succ := make([]string, 0, len(g.out[from]))
	for to := range g.out[from] {
		succ = append(succ, to)
	}
	slices.Sort(succ)
	return succ
}

// nodeSet is a set of node names, such as the nodes reachable from one of them.
type nodeSet map[string]bool

// intersect returns the names in both s and other, in name order, or nil if
// there are none.
func (s nodeSet) intersect(other nodeSet) []string {
	var shared []string
	for n := range s {
		if other[n] {
			shared = append(shared, n)
		}
	}
	slices.Sort(shared)
	return shared
}

// reach returns the set of nodes reachable from node (including node) along
// adjacency: g.out for dependencies, g.in for dependents.
func reach(adjacency map[string]map[string]int, node string) nodeSet {
	seen := nodeSet{node: true}
	stack := []string{node}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for m := range adjacency[n] {
			if !seen[m] {
				seen[m] = true
				stack = append(stack, m)
			}
		}
	}
	return seen
}

// Component returns the nodes in node's strongly connected component: those it
// reaches and that reach it. It is just {node} if node is in no cycle, or is
// not part of the graph at all.
func (g *Graph) Component(node string) []string {
	return reach(g.out, node).intersect(reach(g.in, node))
}

// Neighborhood returns the nodes within depth edges of any of centers,
// following edges in either direction. Nodes are added nearest first, in name
// order within a distance, until limit is reached; truncated says whether that
// left some out.
func (g *Graph) Neighborhood(centers []string, depth, limit int) (nodes []string, truncated bool) {
	seen := map[string]bool{}
	var frontier []string
	for _, c := range centers {
		if !seen[c] {
			seen[c] = true
			frontier = append(frontier, c)
		}
	}
	slices.Sort(frontier)
	for d := 0; ; d++ {
		for _, n := range frontier {
			if len(nodes) == limit {
				return nodes, true
			}
			nodes = append(nodes, n)
		}
		if d >= depth || len(frontier) == 0 {
			return nodes, false
		}
		var next []string
		for _, n := range frontier {
			for _, adj := range g.adjacencies() {
				for m := range adj[n] {
					if !seen[m] {
						seen[m] = true
						next = append(next, m)
					}
				}
			}
		}
		slices.Sort(next)
		frontier = next
	}
}
