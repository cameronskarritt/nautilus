---
name: database-queries
description: Write and review Go database queries using project conventions. Use for static SQL, dynamic querybuilder filters, tenant-scoped lookups, soft deletes, partial updates, keyset pagination, row scanning, no-row behavior, or cached Static/Param queries. Parameterize values, preserve ownership boundaries, normalize cursor types, and keep PostgreSQL and SQLite compatibility where practical.
---

# Database Queries

## Choose SQL or Querybuilder

Use plain SQL when query structure is fixed:

```go
query := `
	SELECT id, external_id, name, created_at
	FROM widgets
	WHERE external_id = $1
	  AND organization_id = $2
	  AND deleted_at IS NULL;
`
```

Use `querybuilder` when filters, update fields, or keyset cursor conditions are
dynamic. Keep explicit SQL for fixed joins, CTEs, subqueries, atomic upserts,
and locking operations even when the query is longer.

Read [references/querybuilder.md](references/querybuilder.md) when using builder
conditions, `Update`, `Delete`, `Static`, `Param`, or a non-default dialect.

## Keep Policy in Go and Atomicity in SQL

Keep application policy in Go:

- validation and allowed state transitions
- status-dependent or optional field selection
- authorization decisions and feature behavior
- response shaping and workflow orchestration

Keep atomicity and concurrency-sensitive work in SQL:

- tenant, ownership, parent, and soft-delete predicates
- unique constraints and concurrency-safe `ON CONFLICT` behavior
- compare-and-swap predicates, fences, leases, and `FOR UPDATE SKIP LOCKED`
- joins, subqueries, CTEs, aggregates, and set-based writes that operate on
  related rows
- database-generated timestamps, identifiers, counters, and sequences when
  they must be atomic with the write

Never add application-defined SQL functions, stored procedures, or triggers.
Implement reusable behavior and validation in Go. Built-in SQL expressions and
database primitives remain acceptable when they are part of an atomic query.

Do not split a correct atomic query into read-decide-write steps merely to make
the SQL shorter or move every expression into Go. Query length and the presence
of a CTE, join, subquery, or `ON CONFLICT` clause are not themselves problems.
Refactor when SQL encodes application policy, duplicates conditions, contains
unreachable clauses, or uses conditional expressions for decisions that can
safely be made before building the query.

Treat `CASE WHEN` in writes as a review signal. Prefer choosing the fields in Go
and using `UpdateBuilder.Set` for dynamic updates. Retain a conditional SQL
expression only when it is necessary to preserve a single atomic operation, and
document that constraint.

## Preserve Data Boundaries

- Include the relevant user, organization, or parent identifier in every
  user-scoped lookup and list.
- Filter `deleted_at IS NULL` for soft-deletable records unless intentionally
  querying deleted data.
- Parameterize values. Never interpolate request values into SQL.
- Allowlist any dynamic column or sort selection before passing it to a builder.
- Keep selected columns and scan destinations in the same order.
- Wrap build, query, and scan errors with operation context.

Treat `sql.ErrNoRows` consistently. Project lookups and `RETURNING` writes
normally return `(nil, nil)` for a missing record:

```go
if errors.Is(err, sql.ErrNoRows) {
	return nil, nil
}
```

For `Exec` writes, inspect `RowsAffected` when callers must distinguish
not-found from success.

## Scan Rows

Use `database.ScanRows` so row closure and iteration errors are handled
consistently:

```go
var widgets []*Widget
err = database.ScanRows(rows, func(row database.Row) error {
	widget := new(Widget)
	if err := row.Scan(
		&widget.ID,
		&widget.ExternalID,
		&widget.Name,
		&widget.CreatedAt,
	); err != nil {
		return errors.Wrap(err, "unable to scan widget")
	}

	widgets = append(widgets, widget)
	return nil
})
```

Initialize response slices when the JSON contract requires `[]` instead of
`null`.

## Build Dynamic Filters

Start with required boundary predicates, then add optional filters:

```go
filters := querybuilder.Eq{
	"organization_id": organizationID,
	"deleted_at":      nil,
}
if opts.Status.IsSet() {
	filters["status"] = opts.Status.Data
}

query, args, err := querybuilder.
	Select("id", "external_id", "status", "created_at").
	From("widgets").
	Where(filters).
	Build()
```

Do not make ownership filters optional when authorization depends on them.

## Implement Keyset Pagination

Parse HTTP parameters in the handler. Pass `pagination.Params` into the
database package; do not pass `*http.Request` into data access code.

Use a unique, immutable tiebreaker:

```go
query, args, err := querybuilder.
	Select("id", "external_id", "created_at").
	From("widgets").
	Where(querybuilder.Eq{
		"organization_id": organizationID,
		"deleted_at":      nil,
	}).
	OrderBy(
		querybuilder.Desc, "created_at",
		querybuilder.Asc, "id",
	).
	Paginate(params).
	Build()
```

`Paginate` requests `limit + 1`; pass all scanned rows to `pagination.Build`.
Build the next cursor from every `ORDER BY` column.

Validate the cursor before building SQL:

- Require exactly the expected order-column keys.
- Convert JSON-decoded strings and `float64` values back to the Go types the
  database driver expects, such as `time.Time` and `int`.
- Reject malformed or incomplete cursors instead of silently degrading the
  ordering condition.
- Discard or reject a cursor when its associated filters change.

Do not pass raw decoded `map[string]any` values into typed timestamp or integer
comparisons.

## Build Partial Updates

Use `optional.Optional` values with `UpdateBuilder.Set`:

```go
query, args, err := querybuilder.Update("widgets").
	Set(
		"name", opts.Name,
		"status", opts.Status,
		"updated_at", querybuilder.Expr("CURRENT_TIMESTAMP"),
	).
	Where(querybuilder.Eq{
		"id":         id,
		"deleted_at": nil,
	}).
	Returning("id", "external_id", "name", "status", "updated_at").
	Build()
```

Honor the API contract for empty patches. Reject one before adding the timestamp
when an empty update is invalid. When an empty patch intentionally means
"touch," allow the timestamp-only update and do not flag it as builder misuse.

Use `CURRENT_TIMESTAMP`, not `now()`, for PostgreSQL/SQLite compatibility.
Apply it to updates, soft deletes, and timestamp comparisons.

## Review Checklist

- Choose plain SQL unless structure is genuinely dynamic.
- Keep business-policy decisions in Go without decomposing atomic database work.
- Allow upserts, locks, leases, CTEs, subqueries, and set operations when they
  preserve atomicity, concurrency, idempotency, or efficient relational work.
- Never add application-defined SQL functions, stored procedures, or triggers.
- Scope every user-facing query to its owner or tenant.
- Filter soft deletes consistently.
- Parameterize values and allowlist dynamic identifiers.
- Align selected columns and scan destinations.
- Handle `sql.ErrNoRows` and zero affected rows deliberately.
- Use a stable composite keyset order.
- Validate and type-normalize every cursor column.
- Handle empty partial updates according to the API's no-op or touch contract.
- Wrap build, query, and scan failures with context.
