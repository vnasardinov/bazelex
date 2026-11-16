package engine

import (
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
	EDB, Derived int
	Relations    []Relation
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
			rows, err := e.db.QueryContext(ctx, q.sql)
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
	e.facts.atoms = atoms
	return atoms, nil
}

// evalDatalog evaluates program over the given facts and returns the derived
// relations. The facts are EDB predicates the program may use; any other
// predicate it mentions must be declared by a rule in the program.
func evalDatalog(program string, edb []ast.Atom, limit int) (*DatalogResult, error) {
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
		slices.SortFunc(rel.Rows, func(a, b []string) int {
			for i := range a {
				if c := strings.Compare(a[i], b[i]); c != 0 {
					return c
				}
			}
			return 0
		})
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
