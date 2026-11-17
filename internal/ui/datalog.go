package ui

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"

	"github.com/vnasardinov/bazelex/internal/engine"
)

// maxDatalogRows bounds one displayed table, not the evaluation: the engine
// returns every derived row, and the page shows only the first few.
const maxDatalogRows = 200

//go:embed datalog.html
var datalogHTML string

var datalogTmpl = template.Must(template.New("datalog").Parse(datalogHTML))

// datalogProgram is one of the example programs offered on the page.
type datalogProgram struct {
	Title       string
	Description string
	Program     string
}

var datalogPrograms = []datalogProgram{
	{
		Title:       "Transitive dependencies",
		Description: `Everything a target pulls in, following deps and embed edges. reachable is the transitive closure of dep, which is exactly the recursion Datalog is good at.`,
		Program: `# dep is the rule-to-rule dependency relation the other pages use.
dep(X, Y) :- edge(X, Y, "deps").
dep(X, Y) :- edge(X, Y, "embed").

reachable(X, Y) :- dep(X, Y).
reachable(X, Z) :- dep(X, Y), reachable(Y, Z).
`,
	},
	{
		Title:       "Rules nothing depends on",
		Description: `Top-level rules: the entry points of the build. This one uses the loaded dep facts and negation, which Mangle evaluates by stratification.`,
		Program: `needed(Y) :- dep(X, Y).
leaf(X) :- target(X, "rule", _), !needed(X).
`,
	},
	{
		Title:       "Hidden dependency cycles",
		Description: `Bazel forbids cycles among targets, not among packages. The quotient facts expose the graph at other granularities: qmember("scc", Component, Package) groups packages by dependency cycle, and qedge("scc", From, To, N) is the component graph you get by collapsing each cycle, which is always acyclic.`,
		Program: `# Which dependency cycle a package belongs to; a component with more
# than one package is a cycle that the target graph hides.
in_component(Pkg, Node) :- qmember("scc", Node, Pkg).

# Dependencies between components, i.e. the package graph with each
# dependency cycle collapsed to a single node.
component_dep(From, To) :- qedge("scc", From, To, _).
`,
	},
}

// datalogView is what datalog.html renders.
type datalogView struct {
	DB      string
	Program string
	Samples []datalogProgram
	Err     string
	Ran     bool
	Result  *engine.DatalogResult
}

func (s *server) datalog(w http.ResponseWriter, r *http.Request) {
	v := datalogView{DB: s.dbName, Samples: datalogPrograms, Program: r.URL.Query().Get("program")}
	if v.Program == "" {
		// Nothing to run yet; offer the first example in the editor.
		v.Program = datalogPrograms[0].Program
	} else {
		v.Ran = true
		if res, err := s.eng.Datalog(r.Context(), v.Program); err != nil {
			v.Err = err.Error()
		} else {
			capRelations(res, maxDatalogRows)
			v.Result = res
		}
	}
	if err := datalogTmpl.Execute(w, v); err != nil {
		log.Printf("render: %v", err)
	}
}

// capRelations keeps at most n rows of each relation, leaving Total intact so
// the page can say how many were dropped.
func capRelations(r *engine.DatalogResult, n int) {
	for i := range r.Relations {
		if rel := &r.Relations[i]; len(rel.Rows) > n {
			rel.Rows = rel.Rows[:n]
		}
	}
}
