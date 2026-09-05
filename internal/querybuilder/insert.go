package querybuilder

import (
	"fmt"
	"strings"

	"nautilus/internal/errors"
)

// InsertBuilder holds INSERT query state.
type InsertBuilder struct {
	table      string
	dialect    PlaceholderDialect
	sets       []setClause
	columns    []string
	rows       [][]any
	onConflict *onConflictClause
	returning  []string
}

type onConflictClause struct {
	columns   []string
	doNothing bool
	updates   []setClause
}

// Insert creates a new INSERT builder for the given table.
func Insert(table string) *InsertBuilder {
	return &InsertBuilder{
		table: table,
	}
}

// Dialect sets the placeholder dialect for this builder.
func (b *InsertBuilder) Dialect(d PlaceholderDialect) *InsertBuilder {
	b.dialect = d
	return b
}

// Set adds one or more (column, value) pairs to the INSERT, slog-style.
//
//	.Set("name", "john", "email", "x@y", "bio", optional.Empty[string]())
//
// Each value may be:
//   - an Expr (rendered inline, no placeholder)
//   - an OptionalValue (skipped when unset; inner value used when set)
//   - any other value (rendered as a placeholder + arg)
//
// Panics if len(pairs) is odd or if a key (even-indexed arg) is not a string.
func (b *InsertBuilder) Set(pairs ...any) *InsertBuilder {
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

// Columns sets the column list for Columns/Values-style inserts.
func (b *InsertBuilder) Columns(cols ...string) *InsertBuilder {
	b.columns = append(b.columns, cols...)
	return b
}

// Values appends one row of values. Can be called multiple times for multi-row inserts.
func (b *InsertBuilder) Values(values ...any) *InsertBuilder {
	row := make([]any, len(values))
	copy(row, values)
	b.rows = append(b.rows, row)
	return b
}

// OnConflict sets the conflict target columns for an ON CONFLICT clause.
// Must be followed by DoNothing or DoUpdateSet before Build.
func (b *InsertBuilder) OnConflict(cols ...string) *InsertBuilder {
	if b.onConflict == nil {
		b.onConflict = &onConflictClause{}
	}
	b.onConflict.columns = append(b.onConflict.columns, cols...)
	return b
}

// DoNothing makes the ON CONFLICT clause a DO NOTHING.
func (b *InsertBuilder) DoNothing() *InsertBuilder {
	if b.onConflict == nil {
		b.onConflict = &onConflictClause{}
	}
	b.onConflict.doNothing = true
	return b
}

// DoUpdateSet adds one or more (column, value) pairs to the ON CONFLICT
// DO UPDATE SET clause, slog-style.
//
//	.DoUpdateSet("key_auth", Excluded("key_auth"), "key_p256dh", Excluded("key_p256dh"))
//
// Value handling mirrors Set:
//   - Expr is rendered inline (use Excluded("col") for EXCLUDED.col references)
//   - OptionalValue is skipped when unset, unwrapped when set
//   - any other value is rendered as a placeholder
//
// Panics if len(pairs) is odd or if a key (even-indexed arg) is not a string.
func (b *InsertBuilder) DoUpdateSet(pairs ...any) *InsertBuilder {
	if len(pairs)%2 != 0 {
		panic("querybuilder: DoUpdateSet requires even number of arguments")
	}
	if b.onConflict == nil {
		b.onConflict = &onConflictClause{}
	}
	for i := 0; i < len(pairs); i += 2 {
		column, ok := pairs[i].(string)
		if !ok {
			panic("querybuilder: DoUpdateSet keys must be strings")
		}
		clause, skip := newSetClause(column, pairs[i+1])
		if skip {
			continue
		}
		b.onConflict.updates = append(b.onConflict.updates, clause)
	}
	return b
}

// Returning adds a RETURNING clause to the INSERT query.
func (b *InsertBuilder) Returning(fields ...string) *InsertBuilder {
	b.returning = append(b.returning, fields...)
	return b
}

// Build generates the INSERT query string and args. Returns an error if the
// builder is in an invalid state or if the chain contains Param(N) values
// (which require .Static() to bind).
func (b *InsertBuilder) Build() (string, []any, error) {
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
func (b *InsertBuilder) Static(key string) *StaticQuery {
	return resolveStatic(key, b)
}

func (b *InsertBuilder) buildOnce() (*buildCtx, string, error) {
	if err := b.validate(); err != nil {
		return nil, "", err
	}

	ctx := newBuildCtx(b.dialect)

	columns, rows := b.materializeRows()

	rowClauses := make([]string, 0, len(rows))
	for _, row := range rows {
		placeholders := make([]string, len(row))
		for i, v := range row {
			if expr, ok := v.(Expr); ok {
				placeholders[i] = string(expr)
				continue
			}
			placeholders[i] = ctx.renderValue(v)
		}
		rowClauses = append(rowClauses, "("+strings.Join(placeholders, ", ")+")")
	}

	var query strings.Builder
	query.WriteString("INSERT INTO ")
	query.WriteString(b.table)
	query.WriteString(" (")
	query.WriteString(strings.Join(columns, ", "))
	query.WriteString(") VALUES ")
	query.WriteString(strings.Join(rowClauses, ", "))

	if b.onConflict != nil {
		query.WriteString(" ON CONFLICT")
		if len(b.onConflict.columns) > 0 {
			query.WriteString(" (")
			query.WriteString(strings.Join(b.onConflict.columns, ", "))
			query.WriteString(")")
		}
		if b.onConflict.doNothing {
			query.WriteString(" DO NOTHING")
		} else {
			updateClauses := make([]string, 0, len(b.onConflict.updates))
			for _, s := range b.onConflict.updates {
				if s.isExpr {
					updateClauses = append(updateClauses, fmt.Sprintf("%s = %s", s.column, s.value))
					continue
				}
				updateClauses = append(updateClauses, fmt.Sprintf("%s = %s", s.column, ctx.renderValue(s.value)))
			}
			query.WriteString(" DO UPDATE SET ")
			query.WriteString(strings.Join(updateClauses, ", "))
		}
	}

	if len(b.returning) > 0 {
		query.WriteString(" RETURNING ")
		query.WriteString(strings.Join(b.returning, ", "))
	}
	query.WriteString(";")

	return ctx, query.String(), nil
}

// validate ensures the builder is in a state that can produce valid SQL.
func (b *InsertBuilder) validate() error {
	hasSets := len(b.sets) > 0
	hasColumns := len(b.columns) > 0
	hasRows := len(b.rows) > 0

	if hasSets && (hasColumns || hasRows) {
		return errors.New("cannot mix Set with Columns/Values")
	}
	if hasColumns && !hasRows {
		return errors.New("no values for columns")
	}
	if hasRows && !hasColumns {
		return errors.New("no columns for values")
	}
	if !hasSets && !hasRows {
		return errors.New("no values to insert")
	}
	if hasColumns {
		for _, row := range b.rows {
			if len(row) != len(b.columns) {
				return errors.New("column/value count mismatch")
			}
		}
	}

	if b.onConflict != nil {
		hasUpdates := len(b.onConflict.updates) > 0
		if !b.onConflict.doNothing && !hasUpdates {
			return errors.New("on conflict requires DoNothing or DoUpdateSet")
		}
		if b.onConflict.doNothing && hasUpdates {
			return errors.New("cannot combine DoNothing with DoUpdateSet")
		}
	}

	return nil
}

// materializeRows converts the current builder state into a (columns, rows) pair
// ready for rendering. Set-style inputs are turned into a single row whose
// values may include Expr (rendered inline by the caller) alongside regular
// values that become placeholders.
func (b *InsertBuilder) materializeRows() ([]string, [][]any) {
	if len(b.sets) > 0 {
		columns := make([]string, len(b.sets))
		row := make([]any, len(b.sets))
		for i, s := range b.sets {
			columns[i] = s.column
			row[i] = s.value
		}
		return columns, [][]any{row}
	}
	return b.columns, b.rows
}
