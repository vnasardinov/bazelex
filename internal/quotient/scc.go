package quotient

import (
	"cmp"
	"slices"
)

// ComponentNames maps every node of a graph to the name of its strongly
// connected component, as ComponentIndex computes. Naming a group per node makes
// it a Partition, so a caller that needs the names as well as the quotient can
// hand it to Collapse rather than write the lookup itself.
type ComponentNames map[string]string

// Partition returns the name of node's component: the group name that component
// takes in the condensation.
func (c ComponentNames) Partition(node string) string { return c[node] }

// ComponentIndex returns, for every node of g, the name of its strongly
// connected component. A component is named after its smallest member, which is
// stable and human-readable, and matches how the README's cycle query names one.
//
// It runs Tarjan's algorithm iteratively, so that a deep graph cannot overflow
// the goroutine stack, and visits each node and edge once: O(V + E).
func ComponentIndex(g *Graph) ComponentNames {
	// frame is one node on the DFS stack, its successors, and how far through
	// them the walk has come.
	type frame struct {
		node   string
		succ   []string
		succIx int
	}
	index := map[string]int{}    // node -> discovery order
	low := map[string]int{}      // node -> lowest reachable index
	onStack := map[string]bool{} // node -> on the component stack
	comp := ComponentNames{}     // node -> component name
	var compStack []string       // Tarjan's stack of the open components
	nextIndex := 0               // the discovery index handed out next

	for _, root := range g.Nodes() {
		if _, done := index[root]; done {
			continue
		}
		index[root], low[root] = nextIndex, nextIndex
		nextIndex++
		compStack = append(compStack, root)
		onStack[root] = true
		frames := []frame{{node: root, succ: g.successors(root)}}
		for len(frames) > 0 {
			f := &frames[len(frames)-1]
			if f.succIx < len(f.succ) {
				w := f.succ[f.succIx]
				f.succIx++
				switch wi, seen := index[w]; {
				case !seen:
					// A tree edge: descend into w.
					index[w], low[w] = nextIndex, nextIndex
					nextIndex++
					compStack = append(compStack, w)
					onStack[w] = true
					frames = append(frames, frame{node: w, succ: g.successors(w)})
				case onStack[w]:
					// A back edge to a node still on the stack: w is in the
					// current component.
					low[f.node] = min(low[f.node], wi)
				}
				continue
			}
			// Every successor of v is done; close it out.
			v := f.node
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				parent := frames[len(frames)-1].node
				low[parent] = min(low[parent], low[v])
			}
			if low[v] == index[v] {
				// v roots a component: pop it and everything above it.
				var members []string
				for {
					w := compStack[len(compStack)-1]
					compStack = compStack[:len(compStack)-1]
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
	membersByName := map[string][]string{}
	for node, name := range index {
		membersByName[name] = append(membersByName[name], node)
	}
	comps := make([][]string, 0, len(membersByName))
	for _, members := range membersByName {
		slices.Sort(members)
		comps = append(comps, members)
	}
	slices.SortFunc(comps, func(a, b []string) int { return cmp.Compare(a[0], b[0]) })
	return comps
}
