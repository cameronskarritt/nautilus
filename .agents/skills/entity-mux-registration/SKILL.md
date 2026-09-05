---
name: entity-mux-registration
description: Register and review HTTP route handlers using the project's mux conventions. Use when adding an endpoint, mounting a handler package, organizing public and protected routes, composing nested routers, handling route parameters, or testing route registration.
---

# Entity Mux Registration

Create a handler package under `internal/app/handlers/{domain}/`, give its mux
only the dependencies its handlers use, and mount it in `internal/app/app.go`.
Inspect neighboring muxes and `internal/mux` before choosing names or route
shapes; preserve the domain's established API contract.

## Organize the Handler Package

Start with the files the domain needs rather than creating empty placeholders:

```text
internal/app/handlers/{domain}/
├── mux.go          # Mux, constructor, and Mount
├── handlers.go     # HTTP handlers
├── forms.go        # Request forms, when applicable
├── errors.go       # Domain HTTP errors, when applicable
└── mux_test.go     # Route and middleware contract tests
```

Keep coupled subresources in the same cohesive domain package. A child domain
with an independent lifecycle may expose its own `Mount` and be mounted by its
parent mux.

## Define a Minimal Mux

Use the package's local naming convention (`Mux`, `UserMux`, and so on). Store
only final, ready-to-use dependencies; for example, pass the traced database
constructed by the app instead of wrapping it again in the handler package.

```go
type Mux struct {
	db database.Database
}

func NewMux(db database.Database) *Mux {
	return &Mux{db: db}
}
```

Do not add a dependency for anticipated handlers. Expand the constructor when a
registered handler actually needs it.

## Mount Routes

Create a subrouter for the supplied prefix and use the router's supported path
syntax:

```go
func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	sub.Get("/", m.List)
	sub.Post("/", m.Create)
	sub.Get("/{itemID:<uuid>}", m.Get)
	sub.Patch("/{itemID:<uuid>}", m.Update)
	sub.Delete("/{itemID:<uuid>}", m.Delete)
}
```

Use descriptive parameter names when a route has multiple IDs. Supported
constraints include `<uuid>` and `<int>`:

| Pattern | Matches |
| --- | --- |
| `{itemID}` | Any non-empty path segment |
| `{itemID:<uuid>}` | A canonical or 32-character hexadecimal UUID |
| `{itemID:<int>}` | An integer |

Do not copy `:itemID` syntax from another router library; `internal/mux` treats
it as a literal segment.

Read parameters through the request:

```go
itemID, ok := mux.PathParam(r, "itemID")
if !ok {
	httputil.Error(r.Context(), w, ErrItemNotFound)
	return
}
```

The route constraint rejects malformed IDs before the handler runs. Keep the
presence check so the handler fails safely if it is called outside the router.
Parse the value only when the database or domain API requires a typed ID.

## Place Middleware Deliberately

Middleware order is part of the route contract:

- `SubRouter` copies the parent middleware active when it is created.
- `Use` affects handlers registered after that call.
- A route registered before `Use` remains outside that middleware.
- The router's fallback 404, 405, and OPTIONS handlers are created before
  later `Use` calls and are not wrapped by them.

Separate public and protected routes explicitly:

```go
authMux.Mount(r, "/auth")

r.Use(middleware.RequireSession(db))
r.Use(middleware.AdminOrgOverride(db))

userMux.Mount(r, "/users")
itemMux.Mount(r, "/items")
```

Apply domain-specific middleware before registering the routes it protects:

```go
func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Use(requireCurrentOrganization)

	sub.Get("/", m.List)
	sub.Post("/", m.Create)
}
```

When required context is optional at the session layer, implement a
package-local guard that checks each context value, verifies related values
agree (for example, the member belongs to the current organization), and
returns a stable centralized HTTP error before calling the handler.

If a mux mixes public and private endpoints, register the public endpoints
first, call `Use`, and then register private endpoints. Prefer a private
subrouter guard when session middleware alone does not guarantee required
context such as a current organization or member. Decide whether unauthenticated
404, 405, and OPTIONS responses may disclose route shape; because fallback
handlers bypass later middleware, test that policy explicitly.

## Register in the App

Construct muxes after their dependencies and mount them in the public or
protected section of `internal/app/app.go`:

```go
itemMux := items.NewMux(tracedDB)

// Later, after protected middleware is active:
itemMux.Mount(r, "/items")
```

Avoid putting an organization ID in the path when the authenticated session
already selects the current organization. Whichever scope the API uses, pass it
to every database operation so authorization is enforced by the query rather
than only by a prior handler check.

## Test the Registration Contract

Mount the mux on a real `internal/mux.Router`; calling handler methods directly
does not populate path parameters or verify middleware and method routing.
Cover the behavior relevant to the new surface:

- collection and detail routes reach the expected handler;
- invalid constrained parameters return 404 without invoking the handler;
- unsupported methods return 405 with the expected `Allow` header;
- fallback 404, 405, and OPTIONS responses follow the intended authentication
  and disclosure policy;
- required middleware runs, and middleware order is observable when relevant;
- missing required request context returns a stable HTTP error instead of
  panicking;
- nested paths and parameter names are exact;
- tenant- or organization-owned resources cannot be read or mutated across
  scopes.

Use independent fixtures for destructive cases. Add handler and form unit tests
separately for behavior that does not depend on routing.
