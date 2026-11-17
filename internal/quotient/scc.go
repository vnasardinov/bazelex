package quotient

import (
	"cmp"
	"slices"
)

// ComponentIndex returns, for every node of g, the name of its strongly
// connected component. A component is named after its smallest member, which is
// stable and human-readable, and matches how the README's cycle query names one.
//
// It runs Tarjan's algorithm iteratively, so that a deep graph cannot overflow
// the goroutine stack, and visits each node and edge once: O(V + E).
func ComponentIndex(g *Graph) map[string]string {
	// frame is one node on the DFS stack and its place in that node's list of
	// successors; next is how far we have walked.
	type frame struct {
		node string
		succ []string
		next int
	}
	index := map[string]int{}    // node -> discovery order
	low := map[string]int{}      // node -> lowest reachable index
	onStack := map[string]bool{} // node -> on the component stack
	comp := map[string]string{}  // node -> component name
	var stack []string           // Tarjan's component stack
	counter := 0

	for _, root := range g.Nodes() {
		if _, done := index[root]; done {
			continue
		}
		index[root], low[root] = counter, counter
		counter++
		stack = append(stack, root)
		onStack[root] = true
		work := []frame{{node: root, succ: g.successors(root)}}
		for len(work) > 0 {
			f := &work[len(work)-1]
			if f.next < len(f.succ) {
				w := f.succ[f.next]
				f.next++
				switch wi, seen := index[w]; {
				case !seen:
					// A tree edge: descend into w.
					index[w], low[w] = counter, counter
					counter++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{node: w, succ: g.successors(w)})
				case onStack[w]:
					// A back edge to a node still on the stack: w is in the
					// current component.
					low[f.node] = min(low[f.node], wi)
				}
				continue
			}
			// Every successor of v is done; close it out.
			v := f.node
			work = work[:len(work)-1]
			if len(work) > 0 {
				parent := work[len(work)-1].node
				low[parent] = min(low[parent], low[v])
			}
			if low[v] == index[v] {
				// v roots a component: pop it and everything above it.
				var members []string
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					members = append(members, w)
					if w == v {
						break
					}
				}
				name := slices.Min(members)
				for _, m := range members {
					comp[m] = name
				}
			}
		}
	}
	return comp
}

// Components returns the strongly connected components of g, each a sorted list
// of member names, in order of the components' names (their smallest member).
func Components(g *Graph) [][]string {
	index := ComponentIndex(g)
	byName := map[string][]string{}
	for node, name := range index {
		byName[name] = append(byName[name], node)
	}
	comps := make([][]string, 0, len(byName))
	for _, members := range byName {
		slices.Sort(members)
		comps = append(comps, members)
	}
	slices.SortFunc(comps, func(a, b []string) int { return cmp.Compare(a[0], b[0]) })
	return comps
}
