// Package engine runs exploration queries over a database written by bazelex:
// a custom SQL statement, or a Mangle (Datalog) program over the build graph.
//
// It is the machinery behind both the web UI and the command-line interface. It
// has no presentation concerns of its own: it returns neutral tables of values
// and leaves formatting (HTML, Markdown, JSON, CSV) to the front-end.
package engine

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	// DefaultTimeout bounds each query and the Datalog fact load when Config
	// does not say otherwise. Mangle's EvalProgram takes no context.
	DefaultTimeout = 120 * time.Second
	// DefaultMaxFacts bounds how many facts a Datalog program may derive when
	// Config does not say otherwise. Datalog always terminates, but the
	// transitive closure of a large graph is quadratic in rules, so an
	// unbounded program can still take very long and need a lot of memory.
	DefaultMaxFacts = 500_000
)

// Config configures an Engine.
type Config struct {
	Timeout  time.Duration // limit on how long each query may run; 0 means DefaultTimeout
	MaxFacts int           // how many facts a Datalog program may derive; 0 means DefaultMaxFacts
}

// normalized fills in the zero values and reports whether the result is usable.
func (c Config) normalized() (Config, error) {
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.MaxFacts == 0 {
		c.MaxFacts = DefaultMaxFacts
	}
	switch {
	case c.Timeout < 0:
		return Config{}, fmt.Errorf("timeout must be positive, got %v", c.Timeout)
	case c.MaxFacts < 1:
		return Config{}, fmt.Errorf("max facts must be at least 1, got %d", c.MaxFacts)
	}
	return c, nil
}

// Validate reports whether c, with defaults applied, is usable.
func (c Config) Validate() error {
	_, err := c.normalized()
	return err
}

// Engine runs exploration queries over one database. It is safe for concurrent
// use: the database must be opened read-only, so concurrent readers are fine,
// and the caches it keeps (graph facts, the quotient graphs) are built once and
// never change.
type Engine struct {
	db       *sql.DB
	timeout  time.Duration
	maxFacts int

	facts  edbCache   // graph facts for Datalog, loaded on first use (datalog.go)
	graphs graphCache // quotient graphs of the build graph (quotient.go)
}

// Open returns an Engine over db, which must be a database written by bazelex.
// It returns an error if cfg is not usable.
func Open(db *sql.DB, cfg Config) (*Engine, error) {
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	return &Engine{db: db, timeout: cfg.Timeout, maxFacts: cfg.MaxFacts}, nil
}

// Table is a SQL result set. Values are kept as scanned, so a caller can format
// them for its medium: nil for NULL, int64 or float64 for numbers, string or
// []byte for text.
type Table struct {
	Columns []string
	Rows    [][]any
}

// Query runs the statement and returns all of its rows. args are bound to the
// statement's parameters.
func (e *Engine) Query(ctx context.Context, query string, args ...any) (*Table, error) {
	t, _, err := e.QueryPage(ctx, 0, 0, query, args...)
	return t, err
}

// QueryPage runs the statement, skips offset rows and returns at most limit of
// them (0 for all), reporting whether any rows follow the ones returned.
// Front-ends use it to paginate or to cap large results without reading a whole
// result set into memory first.
func (e *Engine) QueryPage(ctx context.Context, offset, limit int, query string, args ...any) (*Table, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	t := &Table{}
	if t.Columns, err = rows.Columns(); err != nil {
		return nil, false, err
	}

	skip := offset
	for rows.Next() {
		if skip > 0 {
			skip--
			continue
		}
		// The page is full, so this row is the probe: its existence is the
		// only thing we need from it.
		if limit > 0 && len(t.Rows) == limit {
			return t, true, nil
		}
		row, err := scanRow(rows, len(t.Columns))
		if err != nil {
			return nil, false, err
		}
		t.Rows = append(t.Rows, row)
	}
	return t, false, rows.Err()
}

// scanRow reads the current row of rows, which has n columns.
func scanRow(rows *sql.Rows, n int) ([]any, error) {
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return vals, nil
}

// scanStrings is scanRow into strings: the callers that read only names, labels
// and counts, so that scanRow's destination type is the only difference.
func scanStrings(rows *sql.Rows, n int) ([]string, error) {
	vals := make([]string, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return vals, nil
}

// RuleLabels returns at most max rule labels in label order. It powers input
// autocompletion in the web UI.
func (e *Engine) RuleLabels(ctx context.Context, max int) ([]string, error) {
	t, err := e.Query(ctx, `SELECT label FROM target WHERE kind = 'rule' ORDER BY label LIMIT ?`, max)
	if err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		if s, ok := r[0].(string); ok {
			labels = append(labels, s)
		}
	}
	return labels, nil
}

// Format returns v as text for a human or a text-based format: "NULL" for a
// NULL value, the text of a string or byte slice, and the natural form of a
// number.
func Format(v any) string {
	switch v := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

// IsNumber reports whether v is a numeric value, so callers can right-align it.
func IsNumber(v any) bool {
	switch v.(type) {
	case int64, float64:
		return true
	}
	return false
}
