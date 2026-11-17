package quotient

// Partition assigns each node a group name. Nodes that share a name collapse
// into one node of the quotient. A declared partition, such as package or
// repository, is a function of the node name alone; a computed one, such as the
// strongly connected components, is derived from the graph.
type Partition func(node string) string

// Of returns the quotient of g by part: one node per group name, and an edge
// a -> b whose weight is the sum of the weights of the edges from a node in a
// to a node in b. Edges whose ends fall in the same group are dropped. Groups
// with no cross-group edge are kept as isolated nodes, so the quotient lists
// every group.
func Of(g *Graph, part Partition) *Graph {
	q := New()
	for _, node := range g.Nodes() {
		from := part(node)
		q.AddNode(from)
		for _, d := range g.Deps(node) {
			if to := part(d.To); to != from {
				q.Add(from, to, d.N)
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
	index := ComponentIndex(g)
	return Of(g, func(node string) string { return index[node] })
}
