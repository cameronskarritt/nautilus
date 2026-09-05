---
name: backend-core-service-interfaces
description: Design and review minimal Go service interfaces that follow project conventions. Use when creating a core abstraction, extracting an implementation boundary, defining an interface for dependency injection, or reviewing interface shape. Put context.Context first, use pointer options structs for optional parameters, return domain types and meaningful errors, and keep external dependencies out of core interfaces.
---

# Core Service Interfaces

## Design the Boundary

1. Define an interface only when a consumer needs substitution, isolation, or a
   stable boundary.
2. Keep the interface focused on one capability. Split unrelated operations.
3. Place the interface and its domain types in a cohesive core package, separate
   from provider-specific implementations.
4. Prefer project and standard-library types in method signatures. Do not leak
   provider SDK types into core packages.

Use an existing concrete type directly when an interface would only add
indirection.

## Shape Methods

- Put `context.Context` first on operations that perform work.
- Return an error when the operation can fail; do not add meaningless errors to
  infallible lookups.
- Use domain request types when several required parameters belong together.
- Use a pointer options struct for optional parameters.
- Use `optional.Optional[T]` when omission must differ from a zero value.
- Return domain types rather than implementation-specific response types.

```go
package search

import "context"

type Index[T any] interface {
	Index(ctx context.Context, docs []Document[T]) error
	Delete(ctx context.Context, ids []string) error
	Search(
		ctx context.Context,
		query string,
		opts *SearchOptions,
	) ([]Result[T], error)
}
```

Keep options beside the interface:

```go
type SearchOptions struct {
	Limit optional.Optional[int]
	Mode  optional.Optional[Mode]
}
```

Pass `nil` when no options are needed. Do not replace a stable options struct
with variadic functional options.

## Keep Implementations Behind the Interface

Organize provider implementations beneath or beside the core package:

```text
internal/search/
├── search.go
└── elastic/
    └── elastic.go
```

The implementation may depend on an external SDK. The core interface must not.

Use focused interfaces for distinct roles. For example,
`internal/queue/queue.go` defines separate `Publisher`, `Consumer`, and `Broker`
interfaces instead of one provider-shaped interface.

## Define Domain Errors

Declare stable, package-level sentinel errors only when callers need to branch
on the failure:

```go
var ErrNotFound = errors.New("document not found")
```

Wrap implementation failures with context while preserving errors callers must
inspect.

## Avoid

Do not expose a provider client:

```go
type Store interface {
	Put(ctx context.Context, client *s3.Client, key string) error
}
```

Accept domain inputs instead:

```go
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, opts *PutOptions) error
}
```

Do not omit context from work that may block:

```go
type Store interface {
	Put(key string, body io.Reader) error
}
```

Do not grow an interface for hypothetical future providers. Add methods when a
real consumer requires them.

## Review Checklist

- Confirm the interface represents a real boundary.
- Keep the method set minimal and cohesive.
- Put `context.Context` first on work-performing methods.
- Use request or options structs where they improve the signature.
- Keep provider SDK types out of the core package.
- Return domain types and meaningful errors.
- Define only caller-actionable sentinel errors.
- Verify at least one implementation satisfies the interface.
