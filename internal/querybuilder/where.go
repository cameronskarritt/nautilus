package querybuilder

import (
	"fmt"
	"slices"
	"strings"
)

// Op represents a SQL comparison operator.
type Op string

const (
	OpEq   Op = "="
	OpNe   Op = "<>"
	OpGt   Op = ">"
	OpGte  Op = ">="
	OpLt   Op = "<"
	OpLte  Op = "<="
	OpLike Op = "LIKE"
	OpIn   Op = "IN"
)

// Conditioner is implemented by types that can produce WHERE conditions.
type Conditioner interface {
	Conds() []Cond
}

// Cond represents a WHERE condition.
type Cond struct {
	column string
	op     Op
	value  any
	isNull bool

	// orGroup holds conditions to be joined with OR.
	// When set, the other fields are ignored and this Cond
	// represents (cond1 OR cond2 OR ...).
	orGroup []Cond

	// andGroup holds conditions to be joined with AND (wrapped in parentheses).
	// When set, the other fields are ignored and this Cond
	// represents (cond1 AND cond2 AND ...).
	andGroup []Cond
}

// Conds implements Conditioner for a single condition.
func (c Cond) Conds() []Cond {
	return []Cond{c}
}

// Eq is a map of column to value for equality conditions.
// Keys are sorted for deterministic output.
// Nil values generate IS NULL conditions.
type Eq map[string]any

// Conds converts the Eq map to a slice of conditions.
func (e Eq) Conds() []Cond {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	conds := make([]Cond, 0, len(e))
	for _, column := range keys {
		value := e[column]
		op := OpEq
		isNull := value == nil

		if inv, ok := value.(InList); ok {
			op = OpIn
			value = []any(inv)
		}

		conds = append(conds, Cond{
			column: column,
			op:     op,
			value:  value,
			isNull: isNull,
		})
	}
	return conds
}

// Or groups conditions with OR instead of AND.
type Or []Conditioner

// Conds implements Conditioner, returning a single Cond with orGroup set.
func (o Or) Conds() []Cond {
	var allConds []Cond
	for _, c := range o {
		allConds = append(allConds, c.Conds()...)
	}
	return []Cond{{orGroup: allConds}}
}

// Ne creates a not-equal condition.
func Ne(column string, value any) Cond {
	return Cond{column: column, op: OpNe, value: value}
}

// Gt creates a greater-than condition.
func Gt(column string, value any) Cond {
	return Cond{column: column, op: OpGt, value: value}
}

// Gte creates a greater-than-or-equal condition.
func Gte(column string, value any) Cond {
	return Cond{column: column, op: OpGte, value: value}
}

// Lt creates a less-than condition.
func Lt(column string, value any) Cond {
	return Cond{column: column, op: OpLt, value: value}
}

// Lte creates a less-than-or-equal condition.
func Lte(column string, value any) Cond {
	return Cond{column: column, op: OpLte, value: value}
}

// Like creates a LIKE condition.
func Like(column string, value any) Cond {
	return Cond{column: column, op: OpLike, value: value}
}

// InList wraps values for IN conditions within Eq maps.
type InList []any

// In creates an InList from variadic arguments.
func In(values ...any) InList {
	return InList(values)
}

type whereClause struct {
	column   string
	op       Op
	value    any
	isNull   bool
	orGroup  []whereClause
	andGroup []whereClause
}

// condToWhereClause converts a Cond to a whereClause, handling orGroup and andGroup recursively.
func condToWhereClause(cond Cond) whereClause {
	if len(cond.orGroup) > 0 {
		orClauses := make([]whereClause, 0, len(cond.orGroup))
		for _, c := range cond.orGroup {
			orClauses = append(orClauses, condToWhereClause(c))
		}
		return whereClause{orGroup: orClauses}
	}
	if len(cond.andGroup) > 0 {
		andClauses := make([]whereClause, 0, len(cond.andGroup))
		for _, c := range cond.andGroup {
			andClauses = append(andClauses, condToWhereClause(c))
		}
		return whereClause{andGroup: andClauses}
	}
	return whereClause{
		column: cond.column,
		op:     cond.op,
		value:  cond.value,
		isNull: cond.isNull,
	}
}

// buildCtx carries shared state across the rendering pipeline so that nested
// renderers (OR/AND groups, IN lists) and each builder's outer Build share a
// single placeholder counter, args slice, and Param(N) tracker.
//
// args holds concrete (non-Param) values in the order they are emitted. maxN
// is the largest N declared via Param(N). seen[i] is true when Param(i+1) has
// been encountered at least once.
type buildCtx struct {
	dialect PlaceholderDialect
	argNum  int
	args    []any
	maxN    int
	seen    []bool
}

func newBuildCtx(dialect PlaceholderDialect) *buildCtx {
	if dialect == nil {
		dialect = DefaultDialect
	}
	return &buildCtx{
		dialect: dialect,
		argNum:  1,
	}
}

// markParam records a Param(n) encounter, growing seen as needed. Panics when
// n is out of range; this is a programmer error in the chain construction
// caught at .Static() / Build() time.
func (c *buildCtx) markParam(n int) {
	if n < 1 {
		panic(fmt.Sprintf("querybuilder: Param(%d) must be >= 1", n))
	}
	if n > c.maxN {
		c.maxN = n
	}
	for len(c.seen) < n {
		c.seen = append(c.seen, false)
	}
	c.seen[n-1] = true
}

// renderValue emits a placeholder for a single value. For a Param(N) sentinel
// the dialect's placeholder for N is emitted and no arg is consumed; for any
// other value the dialect's next argNum placeholder is emitted and the value
// is appended to args.
func (c *buildCtx) renderValue(v any) string {
	if pr, ok := v.(paramRef); ok {
		c.markParam(pr.n)
		return c.dialect.Placeholder(pr.n)
	}
	p := c.dialect.Placeholder(c.argNum)
	c.argNum++
	c.args = append(c.args, v)
	return p
}

// renderWhereClause renders a single whereClause (which may be an OR or AND
// group) and mutates ctx to track placeholders and Param usage.
func renderWhereClause(w whereClause, ctx *buildCtx) string {
	if len(w.orGroup) > 0 {
		parts := make([]string, 0, len(w.orGroup))
		for _, orClause := range w.orGroup {
			parts = append(parts, renderWhereClause(orClause, ctx))
		}
		return "(" + strings.Join(parts, " OR ") + ")"
	}

	if len(w.andGroup) > 0 {
		parts := make([]string, 0, len(w.andGroup))
		for _, andClause := range w.andGroup {
			parts = append(parts, renderWhereClause(andClause, ctx))
		}
		return "(" + strings.Join(parts, " AND ") + ")"
	}

	if w.isNull {
		return fmt.Sprintf("%s IS NULL", w.column)
	}

	if w.op == OpIn {
		values := w.value.([]any)
		placeholders := make([]string, len(values))
		for i, v := range values {
			placeholders[i] = ctx.renderValue(v)
		}
		return fmt.Sprintf("%s IN (%s)", w.column, strings.Join(placeholders, ", "))
	}

	op := w.op
	if op == "" {
		op = OpEq
	}
	return fmt.Sprintf("%s %s %s", w.column, op, ctx.renderValue(w.value))
}
