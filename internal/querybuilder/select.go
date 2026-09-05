package querybuilder

import (
	"fmt"
	"strings"

	"nautilus/internal/errors"
)

// OrderDirection represents the sort direction for ORDER BY clauses.
type OrderDirection string

const (
	Asc  OrderDirection = "ASC"
	Desc OrderDirection = "DESC"
)

// Paginator provides pagination parameters for keyset pagination.
type Paginator interface {
	GetCursor() map[string]any
	GetLimit() int
}

// SelectBuilder holds SELECT query state.
type SelectBuilder struct {
	fields   []string
	distinct bool
	table    string
	dialect  PlaceholderDialect
	wheres   []whereClause
	groupBy  []string
	having   []whereClause
	orderBy  []orderByClause
	limit    *int
	offset   *int
	suffix   string
}

type orderByClause struct {
	column    string
	direction OrderDirection
}

// Select creates a new SELECT builder with the given fields.
// Use "*" for all columns.
func Select(fields ...string) *SelectBuilder {
	return &SelectBuilder{
		fields: fields,
	}
}

// Distinct adds the DISTINCT keyword to the query.
func (b *SelectBuilder) Distinct() *SelectBuilder {
	b.distinct = true
	return b
}

// From sets the table to select from.
func (b *SelectBuilder) From(table string) *SelectBuilder {
	b.table = table
	return b
}

// Where adds WHERE conditions.
func (b *SelectBuilder) Where(conditions ...Conditioner) *SelectBuilder {
	for _, c := range conditions {
		for _, cond := range c.Conds() {
			b.wheres = append(b.wheres, condToWhereClause(cond))
		}
	}
	return b
}

// GroupBy sets the GROUP BY columns.
func (b *SelectBuilder) GroupBy(columns ...string) *SelectBuilder {
	b.groupBy = append(b.groupBy, columns...)
	return b
}

// Having adds HAVING conditions.
func (b *SelectBuilder) Having(conditions ...Conditioner) *SelectBuilder {
	for _, c := range conditions {
		for _, cond := range c.Conds() {
			b.having = append(b.having, condToWhereClause(cond))
		}
	}
	return b
}

// OrderBy adds one or more (direction, column) pairs to the ORDER BY clause.
//
//	.OrderBy(Desc, "created_at", Asc, "id")
//
// Panics if len(pairs) is odd, if a direction (even-indexed) is not an
// OrderDirection, or if a column (odd-indexed) is not a string.
func (b *SelectBuilder) OrderBy(pairs ...any) *SelectBuilder {
	if len(pairs)%2 != 0 {
		panic("querybuilder: OrderBy requires even number of arguments")
	}
	for i := 0; i < len(pairs); i += 2 {
		direction, ok := pairs[i].(OrderDirection)
		if !ok {
			panic("querybuilder: OrderBy direction args must be OrderDirection")
		}
		column, ok := pairs[i+1].(string)
		if !ok {
			panic("querybuilder: OrderBy column args must be strings")
		}
		b.orderBy = append(b.orderBy, orderByClause{column: column, direction: direction})
	}
	return b
}

// Limit sets the LIMIT clause.
func (b *SelectBuilder) Limit(n int) *SelectBuilder {
	b.limit = &n
	return b
}

// Offset sets the OFFSET clause.
func (b *SelectBuilder) Offset(n int) *SelectBuilder {
	b.offset = &n
	return b
}

// Suffix appends arbitrary SQL to the end of the query.
// Useful for clauses like FOR UPDATE or FOR SHARE.
func (b *SelectBuilder) Suffix(sql string) *SelectBuilder {
	b.suffix = sql
	return b
}

// Dialect sets the placeholder dialect for this builder.
func (b *SelectBuilder) Dialect(d PlaceholderDialect) *SelectBuilder {
	b.dialect = d
	return b
}

// Paginate configures keyset pagination.
// It adds a WHERE condition based on the cursor and OrderBy clauses,
// and sets LIMIT to limit+1 to enable hasMore detection.
//
// The cursor keys must match the columns in OrderBy clauses.
// If cursor is nil or empty, only the limit is set (first page).
//
// For ORDER BY created_at DESC, id ASC with cursor {created_at: X, id: Y}:
// Generates: WHERE (created_at < X OR (created_at = X AND id > Y))
func (b *SelectBuilder) Paginate(p Paginator) *SelectBuilder {
	limit := p.GetLimit()
	limitPlusOne := limit + 1
	b.limit = &limitPlusOne

	cursor := p.GetCursor()
	if len(cursor) == 0 || len(b.orderBy) == 0 {
		return b
	}

	// Build keyset condition from orderBy and cursor
	// For ORDER BY col1 DESC, col2 ASC with cursor {col1: v1, col2: v2}:
	// WHERE (col1 < v1 OR (col1 = v1 AND col2 > v2))
	var orConds []Cond

	for i := 0; i < len(b.orderBy); i++ {
		order := b.orderBy[i]
		cursorValue, ok := cursor[order.column]
		if !ok {
			continue
		}

		// Build condition for this level: all previous columns equal AND this column comparison
		var levelConds []Cond

		// Add equality conditions for all previous columns
		for j := 0; j < i; j++ {
			prevOrder := b.orderBy[j]
			if prevValue, ok := cursor[prevOrder.column]; ok {
				levelConds = append(levelConds, Cond{column: prevOrder.column, op: OpEq, value: prevValue})
			}
		}

		// Add comparison for this column based on sort direction
		var cmp Cond
		if order.direction == Desc {
			cmp = Cond{column: order.column, op: OpLt, value: cursorValue}
		} else {
			cmp = Cond{column: order.column, op: OpGt, value: cursorValue}
		}

		if len(levelConds) == 0 {
			// First column: just the comparison
			orConds = append(orConds, cmp)
		} else {
			// Subsequent columns: AND all previous equals with this comparison
			levelConds = append(levelConds, cmp)
			orConds = append(orConds, Cond{andGroup: levelConds})
		}
	}

	// Add the OR condition as WHERE clause
	if len(orConds) > 0 {
		if len(orConds) == 1 {
			b.Where(orConds[0])
		} else {
			b.Where(Cond{orGroup: orConds})
		}
	}

	return b
}

// Build generates the SELECT query string and args. Returns an error if no
// fields are provided, table is not set, or the chain contains Param(N)
// values (which require .Static() to bind).
func (b *SelectBuilder) Build() (string, []any, error) {
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
// call from any goroutine materializes the SQL; every subsequent .Static(key)
// returns the cached *StaticQuery without rebuilding, so the chain can be
// constructed inline in hot paths.
//
// Panics on any build-time error or on violations of the .Static() contract
// (concrete values mixed with Param, gaps in Param numbering). The key is
// the global identity of the query: reusing the same key for a different
// logical chain silently returns whichever variant was compiled first.
func (b *SelectBuilder) Static(key string) *StaticQuery {
	return resolveStatic(key, b)
}

// buildOnce produces the SQL and a populated buildCtx. It is the shared
// implementation behind Build() and Static().
func (b *SelectBuilder) buildOnce() (*buildCtx, string, error) {
	if len(b.fields) == 0 {
		return nil, "", errors.New("no fields to select")
	}
	if b.table == "" {
		return nil, "", errors.New("no table specified")
	}

	ctx := newBuildCtx(b.dialect)

	var query strings.Builder
	query.WriteString("SELECT ")
	if b.distinct {
		query.WriteString("DISTINCT ")
	}
	query.WriteString(strings.Join(b.fields, ", "))

	query.WriteString(" FROM ")
	query.WriteString(b.table)

	if len(b.wheres) > 0 {
		whereClauses := make([]string, 0, len(b.wheres))
		for _, w := range b.wheres {
			whereClauses = append(whereClauses, renderWhereClause(w, ctx))
		}
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(whereClauses, " AND "))
	}

	if len(b.groupBy) > 0 {
		query.WriteString(" GROUP BY ")
		query.WriteString(strings.Join(b.groupBy, ", "))
	}

	if len(b.having) > 0 {
		havingClauses := make([]string, 0, len(b.having))
		for _, h := range b.having {
			havingClauses = append(havingClauses, renderWhereClause(h, ctx))
		}
		query.WriteString(" HAVING ")
		query.WriteString(strings.Join(havingClauses, " AND "))
	}

	if len(b.orderBy) > 0 {
		orderClauses := make([]string, 0, len(b.orderBy))
		for _, o := range b.orderBy {
			orderClauses = append(orderClauses, fmt.Sprintf("%s %s", o.column, o.direction))
		}
		query.WriteString(" ORDER BY ")
		query.WriteString(strings.Join(orderClauses, ", "))
	}

	if b.limit != nil {
		fmt.Fprintf(&query, " LIMIT %d", *b.limit)
	}

	if b.offset != nil {
		fmt.Fprintf(&query, " OFFSET %d", *b.offset)
	}

	if b.suffix != "" {
		query.WriteString(" ")
		query.WriteString(b.suffix)
	}

	query.WriteString(";")

	return ctx, query.String(), nil
}
