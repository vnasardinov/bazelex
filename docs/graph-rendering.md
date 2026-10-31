# Rendering the package graph

Notes on how the web UI's package graph page (`/graph`) draws its picture, and
the alternatives that were considered and not taken.

## What we use

[`github.com/goccy/go-graphviz`](https://github.com/goccy/go-graphviz) (v0.2.x).
It runs Graphviz compiled to WebAssembly inside the Go process, through
[wazero](https://wazero.io), so there is no cgo and no `dot` binary to install.
The server turns the neighborhood of a package into DOT, renders it to SVG and
inlines the SVG in `graph.html`. Graphviz's `URL` attribute makes each node a
link, so the page needs no JavaScript.

Things to know:

* `MODULE.bazel` has a `go_deps.gazelle_override` for the module, like the one
  for buildtools, so gazelle doesn't try to generate proto rules for its
  checked-in generated code.
* The WASM build has no font metrics, so it estimates text widths. It knows
  Courier, Helvetica and Times, but not `monospace`. Use a known family or the
  node boxes come out too narrow for their labels.
* Layout time grows steeply with the number of edges, and edge labels count
  because each label becomes a layout node. On a sample package in a large
  workspace, 130 packages and about 1000 edges took 90 seconds (native `dot`
  took the same, so it is Graphviz and not the WASM build). With labels dropped,
  `nslimit=1` and `mclimit=0.2`, and at most 60 nodes, a page renders in about
  half a second.  Edge weights are shown as line width and a tooltip instead of
  labels.

## Alternatives not taken

**Shell out to `dot`.** The simplest option, and it gives the same pictures. It
adds a runtime dependency that `bazel run` can't provide, so every user would
need Graphviz installed. It is the fallback if go-graphviz stops being
maintained or a Graphviz feature it lacks is needed.

**Client-side rendering from JSON.** The server would send nodes and edges, and
the browser would draw them with [Cytoscape.js](https://js.cytoscape.org) (with
the ELK or dagre layout), [d3-graphviz](https://github.com/magjac/d3-graphviz)
or [vis-network](https://visjs.github.io/vis-network/). This gives zoom, pan and
expanding a node in place, which the server-rendered page can't. It was
rejected for now because it needs a vendored JavaScript bundle (the page has no
external dependencies) and the page would stop working without JavaScript. Worth
revisiting if the graph view needs real interactivity. Interactive layouts also
need care at the sizes this tool meets, where a force-directed layout of
hundreds of nodes is a hairball.

**A pure-Go layout library.** We know of none with layered (Sugiyama-style)
layout of the quality `dot` has, and layered layout is what dependency graphs
need. Force-directed layouts are easy to write but show no direction or
layering. Writing our own layered layout is a project in itself.

**Other renderers** such as D2 or Mermaid. D2 is a Go library, but it is aimed
at hand-written diagrams and its layout engines are either `dagre` (run through
a JavaScript runtime) or the commercial TALA or ELK. Mermaid needs a browser or
Node. Neither beats Graphviz for machine-generated dependency graphs.
