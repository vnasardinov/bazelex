# Choke points and betweenness

Notes on how to find the targets in a build graph that sit "in the middle" of
many dependency chains. The web UI's "Choke points" query uses the cheapest
measure below; the rest are options if it turns out not to be accurate enough.

## Betweenness

Freeman's betweenness (1977) scores a node v by summing, over all pairs of
nodes s and t, the fraction of shortest s-to-t paths that pass through v.
Brandes' algorithm (2001) computes it exactly in O(V·E) for unweighted graphs.

The product of fan-in and fan-out only looks one hop out, so it can badly
mis-rank a node whose neighbors are themselves leaves.

## Variants, cheapest first

* **Fan-in × fan-out.** What the UI uses today (over `rule_dep`: rule-to-rule
  `deps` and `embed` edges). It is a Henry–Kafura information-flow measure
  (1981); the original squares the product. It is plain `GROUP BY`, with no
  recursion.
* **Ancestors × descendants.** Multiply the number of rules that transitively
  depend on v by the number v transitively depends on. This counts the (s, t)
  pairs connected through v, which is an upper bound on its betweenness, and
  exact when v is a bridge. The "Impact" and "Heaviest targets" queries already
  compute both numbers, but it is as expensive as "Heaviest targets".
* **Exact path-based betweenness on a DAG.** With no cycles, paths can be
  counted by dynamic programming. With σ(a, b) the number of paths from a to
  b, the score is the sum over s and t of σ(s, v)·σ(v, t)/σ(s, t). Counting
  all paths is arguably more meaningful for builds than counting shortest ones,
  because every path is a real dependency chain. Awkward in SQL, so do it in
  Go.
* **Approximation.** On very large graphs, sample source nodes (Brandes–Pich
  pivots) or sample paths (Riondato–Kornaropoulos) instead of computing exactly.

## Related measures

* **Dominators.** If v dominates t, every path from a root to t goes through v,
  so changing v affects everything it dominates. Probably the most build-relevant
  notion of a choke point. Well defined on a DAG and cheap to compute.
* **Articulation points.** Nodes whose removal disconnects the graph.
* **PageRank on the reversed graph.** Scores targets by how much depends on
  them, weighting dependents by their own importance.
* **k-core decomposition.** Finds the densely entangled core of the graph.

## Caveat: redundant edges

Redundant edges inflate every degree-based number. If `a → b → c` and `a → c`
are both declared, `a → c` is implied. Computing on the transitive reduction
first gives cleaner fan-in and fan-out, at the cost of a further step.
