---
name: database-schema
description: Design and implement repository database domains across schema snapshots, numbered migrations, Go models, data-access packages, and tests. Use when adding or changing tables, columns, indexes, structural constraints, tenant relationships, nullable or JSONB fields, entity packages, or schema registration. Keep fresh and upgraded databases equivalent, preserve migration checksums, keep business validation in Go, and prevent sensitive database fields from leaking through JSON.
---

# Database Schema

## Define the Domain Boundary

Default to one Go package per useful entity. Keep necessarily coupled tables in
one cohesive package and schema file when child records have no meaning outside
the parent.

Examples:

- Keep projects and integrations in the `tenancy.sql` snapshot while exposing
  separate useful packages.
- Keep tightly coupled parent/history tables such as a webhook and its delivery
  records in one `webhooks` package.

Use Go filenames without underscores except for `_test.go`.

## Update Both Schema Paths

Every schema change must support:

1. Fresh databases initialized from snapshot files in
   `internal/database/schema/`.
2. Existing databases upgraded by the next numbered file in
   `internal/database/schema/migrations/`.

Keep the resulting DDL equivalent. Do not edit an existing numbered migration;
applied checksums are verified.

For a new domain:

```text
internal/database/
├── schema/
│   ├── webhooks.sql
│   └── migrations/
│       └── 000008_webhooks.sql
└── webhooks/
    ├── models.go
    ├── webhooks.go
    └── deliveries.go
```

Register the snapshot's bare filename in `schemaFiles` in
`internal/database/schema.go`. Place parents before files that reference them.
Add new dependency-heavy domains near the end unless a later file depends on
them.

## Define Table Identity and Lifecycle

Use the standard columns when the domain needs them:

```sql
id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
external_id UUID NOT NULL DEFAULT uuid_generate_v4(),
created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
deleted_at TIMESTAMPTZ
```

- Use internal integer IDs for joins.
- Expose external UUIDs to clients.
- Use `CURRENT_TIMESTAMP`, not `now()`.
- Add `deleted_at` only for a real soft-delete lifecycle.
- Omit mutable/delete columns from append-only history or event tables.
- Keep append-only behavior in Go and expose no update/delete API.

For claimed jobs, outbox events, or other leased work, store an owner or fencing
token and require it when completing the claim. A timestamp-only lease lets a
stale worker acknowledge work reclaimed by another worker.

Define active uniqueness with a partial index:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_webhooks_organization_url_active
	ON webhooks(organization_id, url)
	WHERE deleted_at IS NULL;
```

Index actual lookup, list, claim, and ordering patterns. Put mandatory tenant
and equality predicates before range/order columns when that matches the query.
Do not add speculative indexes.

## Keep the Schema Declarative

Never add application-defined SQL functions, stored procedures, or triggers.
Implement validation, lifecycle behavior, derived values, and reusable domain
logic in Go.

Do not add `CHECK` constraints or other schema-level validation for business
rules such as enum membership, numeric ranges, state transitions, conditional
nullability, or JSON shape. Validate those rules in Go before writing.

Keep structural storage constraints: primary keys, `NOT NULL`, foreign keys,
uniqueness, and indexes. These define identity, relationships, and atomic
conflict boundaries rather than application policy.

## Model Tenant Ownership

Give organization-owned parent tables a composite candidate key:

```sql
UNIQUE(organization_id, id)
```

Reference both columns from organization-owned children:

```sql
FOREIGN KEY(organization_id, webhook_id)
	REFERENCES webhooks(organization_id, id)
```

This models the parent relationship with its organization.
Include `organization_id` in every public data-access signature and query.

Do not add `ON DELETE CASCADE` unless hard deletion and cascading retention are
explicit requirements.

## Model Nullability and Structured Data

Match SQL nullability in Go:

```go
type Delivery struct {
	ID             int                          `json:"-"`
	ExternalID     string                       `json:"id"`
	OrganizationID int                          `json:"-"`
	LastAttemptAt  optional.Optional[time.Time] `json:"last_attempt_at,omitzero"`
}
```

Use `database.MarshalJSON` and `database.UnmarshalJSON` for JSONB values. A SQL
`NOT NULL` constraint still permits JSON `null`; normalize nil collections and
validate required object or array shapes in Go.

Do not expose secrets, hashes, credential references, encrypted blobs, or
internal ownership IDs through model JSON tags. Use `json:"-"` or an explicit
API DTO.

## Build the Data-Access Package

Start with operations required by real callers. Common signatures are:

```go
func Create(
	ctx context.Context,
	db database.Database,
	organizationID int,
	opts CreateOptions,
) (*Widget, error)

func GetByExternalID(
	ctx context.Context,
	db database.Database,
	organizationID int,
	externalID string,
) (*Widget, error)
```

- Put `context.Context` and `database.Database` first.
- Scope every operation by its owner or organization.
- Filter soft-deleted records consistently.
- Return `(nil, nil)` for `sql.ErrNoRows` on lookups.
- Wrap other failures with operation context.
- Update `updated_at` on mutable writes.
- Inspect affected rows when missing writes must differ from success.
- Provide external-ID lookup when clients receive the external ID.
- Implement soft delete when the schema exposes that lifecycle.

Apply the `database-queries` skill for query construction, partial updates,
pagination, scanning, and no-row behavior.

Create `context.go` only when request middleware actually stores the entity in
context.

## Validate Fresh and Upgrade Paths

Test both schema paths:

- Initialize a fresh database and verify new tables, constraints, and indexes.
- Migrate a database from the previous migration number.
- Verify the new migration record and rerun migration to prove idempotence.
- Confirm the snapshot and migration produce equivalent behavior.
- Test structural cross-organization foreign keys.
- Test active uniqueness and soft-delete reuse.
- Test nullable and JSONB round trips.
- Test sensitive fields are absent from serialized models.
- Test CRUD operations, external-ID lookups, and zero-row behavior.

Run:

```bash
dotenvx run -- go test ./internal/database/<domain> ./internal/database/...
```

Then follow the Go validation gates in `AGENTS.md`.

## Review Checklist

- Choose a cohesive schema file and Go package boundary.
- Update both the snapshot and a new numbered migration.
- Never modify an applied migration.
- Register the snapshot in dependency order.
- Use standard identity and timestamp conventions where appropriate.
- Model organization ownership with composite foreign keys.
- Add only structural constraints and query-backed indexes.
- Fence leased work with scoped query predicates and expose no mutation API for
  append-only history.
- Never add application-defined SQL functions, stored procedures, triggers, or
  `CHECK` constraints.
- Match SQL nullability and JSON shape in Go.
- Hide secrets and internal identifiers from JSON.
- Keep package operations tenant-scoped and lifecycle-complete.
- Test fresh initialization, upgrade, structural relationships, and data access.
