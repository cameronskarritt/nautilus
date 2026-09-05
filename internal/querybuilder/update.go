package querybuilder

import (
	"fmt"
	"strings"

	"nautilus/internal/errors"
)

// UpdateBuilder holds UPDATE query state.
type UpdateBuilder struct {
	table     string
	dialect   PlaceholderDialect
	sets      []setClause
	wheres    []whereClause
	returning []string
}

// Update creates a new UPDATE builder for the given table.
func Update(table string) *UpdateBuilder {
	return &UpdateBuilder{
		table: table,
	}
}

// Dialect sets the placeholder dialect for this builder.
func (b *UpdateBuilder) Dialect(d PlaceholderDialect) *UpdateBuilder {
	b.dialect = d
	return b
}

// Set adds one or more (column, value) pairs to the UPDATE, slog-style.
//
//	.Set("name", "john", "email", "x@y", "bio", optional.Empty[string]())
//
// Each value may be:
//   - an Expr (rendered inline, no placeholder)
//   - an OptionalValue (skipped when unset; inner value used when set)
//   - any other value (rendered as a placeholder + arg)
//
// Panics if len(pairs) is odd or if a key (even-indexed arg) is not a string.
func (b *UpdateBuilder) Set(pairs ...any) *UpdateBuilder {
	if len(pairs)%2 != 0 {
		panic("querybuilder: Set requires even number of arguments")
	}
	for i := 0; i < len(pairs); i += 2 {
		column, ok := pairs[i].(string)
		if !ok {
			panic("querybuilder: Set keys must be strings")
		}
		clause, skip := newSetClause(column, pairs[i+1])
		if skip {
			continue
		}
		b.sets = append(b.sets, clause)
	}
	return b
}

// Where adds WHERE conditions.
func (b *UpdateBuilder) Where(conditions ...Conditioner) *UpdateBuilder {
	for _, c := range conditions {
		for _, cond := range c.Conds() {
			b.wheres = append(b.wheres, condToWhereClause(cond))
		}
	}
	return b
}

// Returning adds a RETURNING clause to the UPDATE query.
func (b *UpdateBuilder) Returning(fields ...string) *UpdateBuilder {
	b.returning = append(b.returning, fields...)
	return b
}

// Build generates the UPDATE query string and args. Returns an error if no
// SET clauses were added or if the chain contains Param(N) values (which
// require .Static() to bind).
func (b *UpdateBuilder) Build() (string, []any, error) {
	ctx, sql, err := b.buildOnce()
	if err != nil {
		return "", nil, err
	}
	if ctx.maxN > 0 {
		return "", nil, errParamRequiresStatic
	}
	return sql, ctx.args, nil
}

// Static compiles the chain into a frozen query cached under key. The first
// call materializes the SQL; subsequent calls with the same key return the
// cached *StaticQuery. Panics on any build-time error or on .Static()
// contract violations.
func (b *UpdateBuilder) Static(key string) *StaticQuery {
	return resolveStatic(key, b)
}

func (b *UpdateBuilder) buildOnce() (*buildCtx, string, error) {
	if len(b.sets) == 0 {
		return nil, "", errors.New("no columns to update")
	}

	ctx := newBuildCtx(b.dialect)

	setClauses := make([]string, 0, len(b.sets))
	for _, s := range b.sets {
		if s.isExpr {
			setClauses = append(setClauses, fmt.Sprintf("%s = %s", s.column, s.value))
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", s.column, ctx.renderValue(s.value)))
	}

	whereClauses := make([]string, 0, len(b.wheres))
	for _, w := range b.wheres {
		whereClauses = append(whereClauses, renderWhereClause(w, ctx))
	}

	query := fmt.Sprintf("UPDATE %s SET %s", b.table, strings.Join(setClauses, ", "))
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	if len(b.returning) > 0 {
		query += " RETURNING " + strings.Join(b.returning, ", ")
	}
	query += ";"

	return ctx, query, nil
}
