package querybuilder

import "nautilus/internal/errors"

// Builder is implemented by every query builder in this package.
// It is the single contract callers depend on when they only need
// to materialize a query (e.g. generic exec helpers, static caches).
type Builder interface {
	Build() (string, []any, error)
}

// errParamRequiresStatic is returned by Build() when the chain contains a
// Param(N) sentinel. Param values are only meaningful inside a .Static()
// chain because Build() has no way to bind runtime placeholders.
var errParamRequiresStatic = errors.New("Param(N) requires .Static(); Build() does not bind runtime placeholders")

// Compile-time assertions that each concrete builder satisfies Builder.
var (
	_ Builder = (*SelectBuilder)(nil)
	_ Builder = (*InsertBuilder)(nil)
	_ Builder = (*UpdateBuilder)(nil)
	_ Builder = (*DeleteBuilder)(nil)
)

// Expr represents a raw SQL expression.
type Expr string

// Excluded returns an Expr referencing the EXCLUDED pseudo-row, suitable for
// use inside DoUpdateSet (e.g. DoUpdateSet("x", Excluded("x")) emits "x = EXCLUDED.x").
func Excluded(column string) Expr {
	return Expr("EXCLUDED." + column)
}

// OptionalValue is satisfied by optional.Optional[T].
type OptionalValue interface {
	IsSet() bool
	GetValue() any
}

type setClause struct {
	column string
	value  any
	isExpr bool
}

// newSetClause builds a setClause from a column/value pair, applying the same
// Expr / OptionalValue handling used across the package. The skip return is
// true when the value is an unset OptionalValue, in which case the clause
// should not be added to the builder.
func newSetClause(column string, value any) (clause setClause, skip bool) {
	if opt, ok := value.(OptionalValue); ok {
		if !opt.IsSet() {
			return setClause{}, true
		}
		value = opt.GetValue()
	}
	clause = setClause{column: column, value: value}
	if _, ok := value.(Expr); ok {
		clause.isExpr = true
	}
	return clause, false
}
