# Querybuilder Reference

## Contents

- [Select](#select)
- [Conditions](#conditions)
- [Pagination](#pagination)
- [Update](#update)
- [Delete](#delete)
- [Static Queries](#static-queries)
- [Dialect and Expressions](#dialect-and-expressions)

## Select

Build dynamic selects with:

```go
query, args, err := querybuilder.
	Select("id", "name", "created_at").
	From("widgets").
	Where(querybuilder.Eq{"organization_id": organizationID}).
	OrderBy(querybuilder.Desc, "created_at", querybuilder.Asc, "id").
	Limit(50).
	Build()
```

Available clauses include:

- `Distinct()`
- `Where(conditions...)`
- `GroupBy(columns...)`
- `Having(conditions...)`
- `OrderBy(direction, column, ...)`
- `Limit(n)` and `Offset(n)`
- `Suffix(sql)` for fixed clauses such as `FOR UPDATE`
- `Paginate(p)`
- `Dialect(d)`

Pass direction/column pairs to `OrderBy`; an odd or mistyped list panics.
Allowlist dynamic column names before calling it.

## Conditions

Use `Eq` for equality, null, and `IN` conditions:

```go
querybuilder.Eq{
	"deleted_at": nil,
	"status":     querybuilder.In("active", "pending"),
}
```

Use comparison helpers:

```go
querybuilder.Gt("age", 18)
querybuilder.Gte("created_at", start)
querybuilder.Lt("created_at", end)
querybuilder.Lte("score", maximum)
querybuilder.Ne("status", "deleted")
querybuilder.Like("email", "%@example.com")
```

Multiple `Where` calls are ANDed. Use `querybuilder.Or` for alternatives:

```go
querybuilder.Or{
	querybuilder.Eq{"status": "active"},
	querybuilder.Eq{"status": "pending"},
}
```

`Eq` renders map keys deterministically, but do not rely on placeholder order
outside the returned `args`.

## Pagination

Call `OrderBy` before `Paginate`. Cursor keys must match every order column.
`Paginate` adds keyset conditions and sets `LIMIT` to `limit + 1`.

For:

```go
OrderBy(
	querybuilder.Desc, "created_at",
	querybuilder.Asc, "id",
)
```

the next-page predicate is equivalent to:

```sql
created_at < $1 OR (created_at = $2 AND id > $3)
```

Validate and convert decoded cursor values before calling `Paginate`.

## Update

Use `Set(column, value, ...)` with even column/value pairs:

```go
querybuilder.Update("widgets").
	Set(
		"name", opts.Name,
		"status", opts.Status,
		"updated_at", querybuilder.Expr("CURRENT_TIMESTAMP"),
	).
	Where(querybuilder.Eq{"id": id, "deleted_at": nil}).
	Returning("id", "updated_at").
	Build()
```

`Set` skips unset `optional.Optional` values and unwraps set values. It renders
`Expr` inline. It panics for odd pairs or non-string column keys. `Build`
returns an error when no columns remain.

UpdateBuilder does not require a `WHERE` clause. Always add the intended scope
unless a whole-table update is explicit.

## Delete

Use hard deletes only for records that should truly disappear:

```go
querybuilder.Delete("push_subscriptions").
	Where(querybuilder.Eq{
		"user_id":  userID,
		"endpoint": endpoint,
	}).
	Returning("id").
	Build()
```

`DeleteBuilder.Build` rejects an unrestricted delete. Prefer a soft-delete
update for user-facing records.

## Static Queries

Use `Static` only for fixed query shapes on hot paths. Replace every runtime
value with contiguous, one-indexed `Param` declarations:

```go
var getWidgetQuery = querybuilder.
	Select("id", "name").
	From("widgets").
	Where(querybuilder.Eq{
		"organization_id": querybuilder.Param(1),
		"external_id":     querybuilder.Param(2),
		"deleted_at":      nil,
	}).
	Static("widgets.get-by-external-id")

query, args := getWidgetQuery.Query(organizationID, externalID)
```

Use a globally unique key for each logical query. The first query stored under
a key wins; reusing a key for another shape can silently return the first one.

Do not mix concrete values with `Param` in a static query. Keep parameters
contiguous from `Param(1)` through `Param(N)`. `Query` panics when the runtime
argument count does not match.

Use `Build`, not `Static`, for optional filters or other dynamic shapes.

## Dialect and Expressions

Builders default to PostgreSQL placeholders. Use `Dialect(d)` when targeting
another supported database.

Use `querybuilder.Expr` only for fixed, trusted SQL:

```go
querybuilder.Expr("CURRENT_TIMESTAMP")
```

Never place request data inside an expression, suffix, table name, column name,
or sort clause.
