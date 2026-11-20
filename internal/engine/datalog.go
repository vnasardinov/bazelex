package engine

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/mangle/analysis"
	"github.com/google/mangle/ast"
	"github.com/google/mangle/engine"
	"github.com/google/mangle/factstore"
	"github.com/google/mangle/parse"

	"github.com/vnasardinov/bazelex/internal/quotient"
)

// The extensional predicates loaded from the database. Labels, kinds and
// attribute names are Mangle *string* constants, not name constants, because a
// Bazel label like "//foo:bar" is not a valid name constant: its leading "//"
// would be an empty path part. So a program writes dep("//foo:bar", X), not
// dep(//foo:bar, X).
var (
	targetPred = ast.PredicateSym{Symbol: "target", Arity: 3} // label, kind, package
	depPred    = ast.PredicateSym{Symbol: "dep", Arity: 2}    // from, to (rule_dep: deps and embed)
	edgePred   = ast.PredicateSym{Symbol: "edge", Arity: 3}   // from, to, attribute

	// The quotient graphs of the build graph, named by their first argument:
	// "package" is the package quotient, and "scc" is its strongly connected
	// components, so a program can reason at package or component granularity
	// instead of at the rule granularity of dep and edge.
	qnodePred   = ast.PredicateSym{Symbol: "qnode", Arity: 2}   // quotient, node
	qedgePred   = ast.PredicateSym{Symbol: "qedge", Arity: 4}   // quotient, from, to, weight
	qmemberPred = ast.PredicateSym{Symbol: "qmember", Arity: 3} // quotient, node, member
)

// The quotient names the facts carry: the first argument of qnode, qedge and
// qmember. A program writes them as strings, e.g. qedge("scc", From, To, N), so
// they are the vocabulary the page documents, not an internal label.
const (
	packageQuotient = "package"
	sccQuotient     = "scc"
)

// edbCache holds the graph's facts, built once from the read-only database.
type edbCache struct {
	mu    sync.Mutex
	atoms []ast.Atom
}

// Relation is one derived predicate's facts, ready to render.
type Relation struct {
	Name    string   // e.g. "reachable/2"
	Columns []string // argument names, from the rule head's variables
	Rows    [][]string
	Total   int // rows before any cap the caller applies
}

// DatalogResult is the outcome of evaluating one program.
type DatalogResult struct {
	// EDB is the number of facts loaded from the database, including those of
	// a predicate the program redefines.
	EDB int
	// Derived is the store's fact count minus EDB. A predicate the program
	// defines is not loaded (evalDatalog filters its facts out), so for a
	// program that redefines a loaded predicate this undercounts what the
	// program derives by that predicate's loaded facts.
	Derived int
	// Relations holds the derived predicates, in symbol order.
	Relations []Relation
}

// Datalog evaluates program over the graph's facts and returns the relations it
// derived. The facts are the EDB predicates target/3, dep/2 and edge/3; any
// other predicate the program mentions must be defined by a rule in it. A
// program that derives more than Config's MaxFacts facts is stopped with an
// error.
func (e *Engine) Datalog(ctx context.Context, program string) (*DatalogResult, error) {
	edb, err := e.datalogFacts(ctx)
	if err != nil {
		return nil, err
	}
	return evalDatalog(program, edb, e.maxFacts)
}

// factQuery is one SELECT over the database and the atom it builds from each
// row.
type factQuery struct {
	sql  string
	atom func([]string) ast.Atom
}

// datalogFacts returns the graph as Datalog facts, reading them from the
// database on first use only: the database is read-only, so they never change.
func (e *Engine) datalogFacts(ctx context.Context) ([]ast.Atom, error) {
	e.facts.mu.Lock()
	defer e.facts.mu.Unlock()
	if e.facts.atoms != nil {
		return e.facts.atoms, nil
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	queries := []factQuery{
		{`SELECT label, kind, package FROM target`,
			func(v []string) ast.Atom {
				return ast.NewAtom(targetPred.Symbol, ast.String(v[0]), ast.String(v[1]), ast.String(v[2]))
			}},
		{`SELECT f.label, t.label FROM rule_dep d
		 JOIN target f ON f.id = d.from_id JOIN target t ON t.id = d.to_id`,
			func(v []string) ast.Atom {
				return ast.NewAtom(depPred.Symbol, ast.String(v[0]), ast.String(v[1]))
			}},
		{`SELECT f.label, t.label, e.attr FROM edge e
		 JOIN target f ON f.id = e.from_id JOIN target t ON t.id = e.to_id`,
			func(v []string) ast.Atom {
				return ast.NewAtom(edgePred.Symbol, ast.String(v[0]), ast.String(v[1]), ast.String(v[2]))
			}},
	}
	var atoms []ast.Atom
	for _, q := range queries {
		if err := func() error {
			rows, err := e.db.QueryContext(ctx, q.sql)
			if err != nil {
				return err
			}
			defer rows.Close()
			cols, err := rows.Columns()
			if err != nil {
				return err
			}
			for rows.Next() {
				vals, err := scanInto[string](rows, len(cols))
				if err != nil {
					return err
				}
				atoms = append(atoms, q.atom(vals))
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	q, err := e.quotientAtoms(ctx)
	if err != nil {
		return nil, err
	}
	atoms = append(atoms, q...)
	e.facts.atoms = atoms
	return atoms, nil
}

// quotientAtoms returns the quotient graphs as facts: qnode and qedge describe
// each quotient, and qmember says which package collapses into which component
// of the condensation. The underlying facts come from PackageGraph and
// CondensedGraph, so a program can reason over packages or over dependency
// cycles without re-deriving them itself.
func (e *Engine) quotientAtoms(ctx context.Context) ([]ast.Atom, error) {
	pkg, err := e.PackageGraph(ctx)
	if err != nil {
		return nil, err
	}
	cond, component, err := e.CondensedGraph(ctx)
	if err != nil {
		return nil, err
	}
	var atoms []ast.Atom
	for _, g := range []struct {
		name  string
		graph *quotient.Graph
	}{{packageQuotient, pkg}, {sccQuotient, cond}} {
		for _, n := range g.graph.Nodes() {
			atoms = append(atoms, ast.NewAtom(qnodePred.Symbol, ast.String(g.name), ast.String(n)))
		}
		for _, from := range g.graph.Nodes() {
			for _, d := range g.graph.Deps(from) {
				atoms = append(atoms, ast.NewAtom(qedgePred.Symbol, ast.String(g.name), ast.String(from), ast.String(d.To), ast.Number(int64(d.Weight))))
			}
		}
	}
	// Packages come sorted, so the members are emitted deterministically.
	for _, p := range pkg.Nodes() {
		atoms = append(atoms, ast.NewAtom(qmemberPred.Symbol, ast.String(sccQuotient), ast.String(component[p]), ast.String(p)))
	}
	return atoms, nil
}

// evalDatalog evaluates program over the given facts and returns the derived
// relations. The facts are EDB predicates the program may use; any other
// predicate it mentions must be declared by a rule in the program.
func evalDatalog(program string, edb []ast.Atom, maxFacts int) (*DatalogResult, error) {
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
	for _, sym := range []ast.PredicateSym{targetPred, depPred, edgePred, qnodePred, qedgePred, qmemberPred} {
		if !defined[sym] {
			extra[sym] = ast.NewSyntheticDeclFromSym(sym)
		}
	}
	info, err := analysis.AnalyzeOneUnit(unit, extra)
	if err != nil {
		return nil, err
	}
	if err := engine.EvalProgram(info, store, engine.WithCreatedFactLimit(maxFacts)); err != nil {
		return nil, err
	}

	syms := make([]ast.PredicateSym, 0, len(info.IdbPredicates))
	for sym := range info.IdbPredicates {
		syms = append(syms, sym)
	}
	slices.SortFunc(syms, func(a, b ast.PredicateSym) int {
		return cmp.Or(strings.Compare(a.Symbol, b.Symbol), cmp.Compare(a.Arity, b.Arity))
	})
	res := &DatalogResult{EDB: len(edb), Derived: store.EstimateFactCount() - len(edb)}
	for _, sym := range syms {
		rel := Relation{Name: fmt.Sprintf("%s/%d", sym.Symbol, sym.Arity), Columns: datalogColumns(info, sym)}
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
		slices.SortFunc(rel.Rows, slices.Compare)
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
