package querybuilder

import (
	"strings"

	"nautilus/internal/errors"
)

// DeleteBuilder holds DELETE query state.
type DeleteBuilder struct {
	table     string
	dialect   PlaceholderDialect
	wheres    []whereClause
	returning []string
}

// Delete creates a new DELETE builder for the given table.
func Delete(table string) *DeleteBuilder {
	return &DeleteBuilder{
		table: table,
	}
}

// Dialect sets the placeholder dialect for this builder.
func (b *DeleteBuilder) Dialect(d PlaceholderDialect) *DeleteBuilder {
	b.dialect = d
	return b
}

// Where adds WHERE conditions.
func (b *DeleteBuilder) Where(conditions ...Conditioner) *DeleteBuilder {
	for _, c := range conditions {
		for _, cond := range c.Conds() {
			b.wheres = append(b.wheres, condToWhereClause(cond))
		}
	}
	return b
}

// Returning adds a RETURNING clause to the DELETE query.
func (b *DeleteBuilder) Returning(fields ...string) *DeleteBuilder {
	b.returning = append(b.returning, fields...)
	return b
}

// Build generates the DELETE query string and args. Returns an error if no
// WHERE conditions were added (an unrestricted DELETE wipes the entire table
// and is almost always a bug; drop down to raw SQL if that is intentional)
// or if the chain contains Param(N) values (which require .Static() to bind).
func (b *DeleteBuilder) Build() (string, []any, error) {
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
func (b *DeleteBuilder) Static(key string) *StaticQuery {
	return resolveStatic(key, b)
}

func (b *DeleteBuilder) buildOnce() (*buildCtx, string, error) {
	if len(b.wheres) == 0 {
		return nil, "", errors.New("DELETE requires at least one WHERE condition")
	}

	ctx := newBuildCtx(b.dialect)

	whereClauses := make([]string, 0, len(b.wheres))
	for _, w := range b.wheres {
		whereClauses = append(whereClauses, renderWhereClause(w, ctx))
	}

	var query strings.Builder
	query.WriteString("DELETE FROM ")
	query.WriteString(b.table)
	query.WriteString(" WHERE ")
	query.WriteString(strings.Join(whereClauses, " AND "))
	if len(b.returning) > 0 {
		query.WriteString(" RETURNING ")
		query.WriteString(strings.Join(b.returning, ", "))
	}
	query.WriteString(";")

	return ctx, query.String(), nil
}
