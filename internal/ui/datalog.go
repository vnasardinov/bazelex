package ui

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/google/mangle/analysis"
	"github.com/google/mangle/ast"
	"github.com/google/mangle/engine"
	"github.com/google/mangle/factstore"
	"github.com/google/mangle/parse"
)

// The extensional predicates the page loads from the database. Labels, kinds
// and attribute names are Mangle *string* constants, not name constants,
// because a Bazel label like "//foo:bar" is not a valid name constant: its
// leading "//" would be an empty path part. So a program writes
// dep("//foo:bar", X), not dep(//foo:bar, X).
var (
	targetPred = ast.PredicateSym{Symbol: "target", Arity: 3} // label, kind, package
	depPred    = ast.PredicateSym{Symbol: "dep", Arity: 2}    // from, to (rule_dep: deps and embed)
	edgePred   = ast.PredicateSym{Symbol: "edge", Arity: 3}   // from, to, attribute
)

const (
	// maxDatalogFacts bounds how many facts a program may derive. Datalog always
	// terminates, but the transitive closure of a large graph is quadratic in
	// rules, so an unbounded program can still take very long and need a lot of
	// memory.
	maxDatalogFacts = 500_000
	// maxDatalogRows bounds one table, not the evaluation.
	maxDatalogRows = 200
)

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
}

// edbCache holds the graph's facts, built once from the read-only database.
type edbCache struct {
	mu    sync.Mutex
	atoms []ast.Atom
}

// datalogFacts returns the graph as Datalog facts, reading them from the
// database on first use only: the database is read-only, so they never change.
func (s *server) datalogFacts(ctx context.Context) ([]ast.Atom, error) {
	s.facts.mu.Lock()
	defer s.facts.mu.Unlock()
	if s.facts.atoms != nil {
		return s.facts.atoms, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	queries := []struct {
		sql   string
		arity int
		atom  func([]string) ast.Atom
	}{
		{`SELECT label, kind, package FROM target`, 3,
			func(v []string) ast.Atom {
				return ast.NewAtom(targetPred.Symbol, ast.String(v[0]), ast.String(v[1]), ast.String(v[2]))
			}},
		{`SELECT f.label, t.label FROM rule_dep d
		 JOIN target f ON f.id = d.from_id JOIN target t ON t.id = d.to_id`, 2,
			func(v []string) ast.Atom {
				return ast.NewAtom(depPred.Symbol, ast.String(v[0]), ast.String(v[1]))
			}},
		{`SELECT f.label, t.label, e.attr FROM edge e
		 JOIN target f ON f.id = e.from_id JOIN target t ON t.id = e.to_id`, 3,
			func(v []string) ast.Atom {
				return ast.NewAtom(edgePred.Symbol, ast.String(v[0]), ast.String(v[1]), ast.String(v[2]))
			}},
	}
	var atoms []ast.Atom
	for _, q := range queries {
		if err := func() error {
			rows, err := s.db.QueryContext(ctx, q.sql)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				vals := make([]string, q.arity)
				ptrs := make([]any, q.arity)
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return err
				}
				atoms = append(atoms, q.atom(vals))
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	s.facts.atoms = atoms
	return atoms, nil
}

// relation is one derived predicate's facts, ready to render.
type relation struct {
	Name    string
	Columns []string
	Rows    [][]string
	Total   int // rows before the display cap
}

// datalogResult is the outcome of evaluating one program.
type datalogResult struct {
	EDB, Derived int
	Relations    []relation
}

// evalDatalog evaluates program over the given facts and returns the derived
// relations. The facts are EDB predicates the program may use; any other
// predicate it mentions must be declared by a rule in the program.
func evalDatalog(program string, edb []ast.Atom, limit int) (*datalogResult, error) {
	unit, err := parse.Unit(strings.NewReader(program))
	if err != nil {
		return nil, err
	}
	// A program may define a predicate that is also loaded, for example to
	// rebuild dep from edge. Then it is the program's relation, so neither
	// declare it nor seed it from the database: Mangle refuses to have a
	// predicate declared and defined at once.
	defined := map[ast.PredicateSym]bool{}
	for _, c := range unit.Clauses {
		defined[c.Head.Predicate] = true
	}
	// The engine adds facts to the store, so each evaluation needs its own copy.
	store := factstore.NewSimpleInMemoryStore()
	for _, a := range edb {
		if !defined[a.Predicate] {
			store.Add(a)
		}
	}
	extra := map[ast.PredicateSym]ast.Decl{}
	for _, sym := range []ast.PredicateSym{targetPred, depPred, edgePred} {
		if !defined[sym] {
			extra[sym] = ast.NewSyntheticDeclFromSym(sym)
		}
	}
	info, err := analysis.AnalyzeOneUnit(unit, extra)
	if err != nil {
		return nil, err
	}
	if err := engine.EvalProgram(info, store, engine.WithCreatedFactLimit(limit)); err != nil {
		return nil, err
	}

	syms := make([]ast.PredicateSym, 0, len(info.IdbPredicates))
	for sym := range info.IdbPredicates {
		syms = append(syms, sym)
	}
	slices.SortFunc(syms, func(a, b ast.PredicateSym) int {
		if c := strings.Compare(a.Symbol, b.Symbol); c != 0 {
			return c
		}
		return a.Arity - b.Arity
	})
	res := &datalogResult{EDB: len(edb), Derived: store.EstimateFactCount() - len(edb)}
	for _, sym := range syms {
		rel := relation{Name: fmt.Sprintf("%s/%d", sym.Symbol, sym.Arity), Columns: datalogColumns(info, sym)}
		if err := store.GetFacts(ast.NewQuery(sym), func(a ast.Atom) error {
			row := make([]string, len(a.Args))
			for i, t := range a.Args {
				row[i] = datalogTerm(t)
			}
			rel.Rows = append(rel.Rows, row)
			return nil
		}); err != nil {
			return nil, err
		}
		rel.Total = len(rel.Rows)
		slices.SortFunc(rel.Rows, func(a, b []string) int {
			for i := range a {
				if c := strings.Compare(a[i], b[i]); c != 0 {
					return c
				}
			}
			return 0
		})
		rel.Rows = rel.Rows[:min(len(rel.Rows), maxDatalogRows)]
		res.Relations = append(res.Relations, rel)
	}
	return res, nil
}

// datalogTerm formats one result value for display: the bare value of a string
// constant (a label, kind or attribute name), or its Mangle syntax otherwise.
func datalogTerm(t ast.BaseTerm) string {
	if c, ok := t.(ast.Constant); ok {
		if s, err := c.StringValue(); err == nil {
			return s
		}
	}
	return t.String()
}

// datalogColumns names the arguments of a derived predicate, after the variable
// names of the rule that defines it ("X", "Y"), or "arg1", ... when it has none.
func datalogColumns(info *analysis.ProgramInfo, sym ast.PredicateSym) []string {
	var cols []string
	if decl, ok := info.Decls[sym]; ok {
		for _, arg := range decl.DeclaredAtom.Args {
			if v, ok := arg.(ast.Variable); ok && !strings.HasPrefix(v.Symbol, "_") {
				cols = append(cols, v.Symbol)
				continue
			}
			cols = append(cols, fmt.Sprintf("arg%d", len(cols)+1))
		}
	}
	for len(cols) < sym.Arity {
		cols = append(cols, fmt.Sprintf("arg%d", len(cols)+1))
	}
	return cols
}

// datalogView is what datalog.html renders.
type datalogView struct {
	DB      string
	Program string
	Samples []datalogProgram
	Err     string
	Ran     bool
	Result  *datalogResult
}

func (s *server) datalog(w http.ResponseWriter, r *http.Request) {
	v := datalogView{DB: s.dbName, Samples: datalogPrograms, Program: r.URL.Query().Get("program")}
	if v.Program == "" {
		// Nothing to run yet; offer the first example in the editor.
		v.Program = datalogPrograms[0].Program
	} else {
		v.Ran = true
		if edb, err := s.datalogFacts(r.Context()); err != nil {
			v.Err = err.Error()
		} else if res, err := evalDatalog(v.Program, edb, maxDatalogFacts); err != nil {
			v.Err = err.Error()
		} else {
			v.Result = res
		}
	}
	if err := datalogTmpl.Execute(w, v); err != nil {
		log.Printf("render: %v", err)
	}
}
