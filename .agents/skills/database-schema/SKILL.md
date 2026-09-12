---
name: database-schema
description: Change database snapshots, numbered migrations, and Go models while preserving the repository schema initialization and upgrade contracts.
---

# Database Schema

## Snapshot and Upgrade Paths

Schema changes must update both snapshot files in `internal/database/schema/`
and a new numbered file in `internal/database/schema/migrations/`. Determine the
next unused migration number from the current tree. Do not change existing
numbered migrations: applied checksums are verified.

Keep fresh initialization and upgrade DDL equivalent. Register a new snapshot's
bare filename in `schemaFiles` in `internal/database/schema.go`, after its parents
and before dependent files. Use cohesive domain packages and schema files;
necessarily coupled parent/history tables may share them. Inspect the existing schema registration for domain-specific grouping.

## Storage Conventions

Use the columns appropriate to the domain:

```sql
id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
external_id UUID NOT NULL DEFAULT uuid_generate_v4(),
created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
deleted_at TIMESTAMPTZ
```

Internal integer IDs serve joins; external UUIDs serve clients. Use
`CURRENT_TIMESTAMP`, not `now()`. Include soft deletion only for a real lifecycle;
append-only history needs no mutable/delete columns or mutation API. Active
uniqueness uses a partial unique index with `WHERE deleted_at IS NULL`. Index
actual query patterns rather than anticipated ones.

Keep structural constraints: primary keys, `NOT NULL`, foreign keys, uniqueness,
and indexes. Application validation and lifecycle policy belong in Go. Do not
add application-defined SQL functions, procedures, triggers, or `CHECK`
constraints for business rules. Preserve atomic database work as described in
`database-queries`.

For organization-owned parent/child relationships, use a candidate key
`UNIQUE(organization_id, id)` and a child foreign key over both organization and
parent IDs. Scope public data access by that organization. Add `ON DELETE CASCADE`
only when hard deletion and cascading retention are requirements.

Leased work needs an owner or fencing token checked on completion; a timestamp
alone lets an old worker acknowledge a reclaimed job.

## Go Models and Access

Match SQL nullability with `optional.Optional[T]` where appropriate. Use
`database.MarshalJSON`/`database.UnmarshalJSON` for JSONB. SQL `NOT NULL` still
allows JSON `null`; normalize collections or validate object/array shapes in Go
when the contract requires them.

Hide secrets, hashes, credential references, encrypted blobs, and internal
ownership IDs with `json:"-"` or an explicit API DTO. Public external IDs normally
use `json:"id"`.

Data access takes `context.Context` then `database.Database`, followed by the
owner scope and operation inputs. Add operations real callers need, including
external-ID lookup or soft deletion when that lifecycle requires them. Apply
`database-queries` for missing rows, updates, and scanning. Add `context.go` only
when middleware actually stores the entity in request context.

## Verification

Verify fresh initialization and upgrade from the previous migration, reusing
existing migration tests where they cover the change. Confirm migration reruns
and snapshot/upgrade behavior agree. Add domain tests only for affected contracts,
such as changed tenant FKs, uniqueness, JSON/nullability, serialization, or CRUD.
Follow AGENTS.md for required validation.
