---
name: database-queries
description: Implement repository database queries using its querybuilder, scoped lookups, partial updates, and keyset pagination contracts.
---

# Database Queries

Use plain SQL for fixed structures, including joins, CTEs, atomic upserts, and
locking. Use `querybuilder` for dynamic filters, update fields, or cursor
conditions. Read [references/querybuilder.md](references/querybuilder.md) when
using builder conditions, `Update`, `Delete`, `Static`, `Param`, or dialects.

## Policy and Atomicity

Keep application validation, allowed transitions, authorization decisions,
optional field selection, and workflow orchestration in Go. Keep tenant/ownership
predicates and concurrency-sensitive work in SQL: unique conflicts,
compare-and-swap, fences, leases, locks, and set-based relational operations.
Database-generated timestamps and counters may remain atomic with the write.

Do not add application-defined SQL functions, procedures, or triggers. Built-in
expressions and database primitives are appropriate for atomic queries. Do not
split atomic SQL into read-decide-write steps just to shorten it or avoid a CTE,
join, subquery, or `ON CONFLICT`.

Prefer selecting update fields in Go over policy-driven `CASE WHEN` writes.
Keep conditional SQL where a single atomic operation requires it; explain
non-obvious concurrency constraints rather than commenting every expression.

## Query Contracts

- Required owner, organization, or parent predicates must remain required in
  user-scoped queries. Filter `deleted_at IS NULL` for soft-deletable records
  unless intentionally querying deleted data.
- Parameterize values and allowlist dynamic column/sort identifiers. Builder
  expressions and suffixes are trusted SQL, not escaping mechanisms.
- Project lookups and `RETURNING` writes normally return `(nil, nil)` for
  `sql.ErrNoRows`. Check `RowsAffected` for `Exec` writes when callers must
  distinguish missing records from success.
- Align selected columns and scan destinations. Use `database.ScanRows` for
  iteration; it handles row closure and iteration errors. Wrap build, query,
  and scan failures with useful operation context without duplicating wrapping.
- Initialize slices when the JSON response contract requires `[]` rather than
  `null`.

## Keyset Pagination

Parse HTTP input in the handler and pass `pagination.Params` into data access.
Use a stable order with a unique immutable tiebreaker. Call `OrderBy` before
`Paginate`; it requests `limit + 1`. Pass all scanned rows to `pagination.Build`
and include every order column in the next cursor.

Validate the cursor before building SQL: require exactly the expected keys and
convert decoded strings/`float64` values to driver types such as `time.Time` and
`int`. Reject malformed or incomplete cursors rather than silently degrading the
ordering predicate. Discard or reject a cursor when its associated filters
change. Raw decoded `map[string]any` values are not suitable for typed timestamp
or integer comparisons.

## Partial Updates

`UpdateBuilder.Set` skips unset `optional.Optional` values and unwraps set ones.
Use `querybuilder.Expr("CURRENT_TIMESTAMP")` for `updated_at`; use
`CURRENT_TIMESTAMP` rather than `now()` for PostgreSQL/SQLite compatibility in
updates, soft deletes, and comparisons.

Honor the empty-patch contract: reject an invalid empty patch before adding the
timestamp, or permit a timestamp-only write when the operation means "touch."
An update builder does not require a WHERE clause; supply the intended scope.
