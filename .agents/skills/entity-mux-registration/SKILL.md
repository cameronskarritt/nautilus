---
name: entity-mux-registration
description: Mount Go endpoints with the custom internal/mux router, including its path syntax and registration-time middleware behavior.
---

# Route Registration

Handler packages live under `internal/app/handlers/{domain}/` and are mounted in
`internal/app/app.go`. Follow neighboring mux naming and route contracts. Give a
mux only dependencies its handlers use; pass the app's traced database rather
than wrapping it again. Create forms/errors files only when needed.

```go
func (m *Mux) Mount(r *mux.Router, prefix string) {
    sub := r.SubRouter(prefix)
    sub.Get("/", m.List)
    sub.Get("/{itemID:<uuid>}", m.Get)
}
```

Supported parameters are `{itemID}`, `{itemID:<uuid>}`, and `{itemID:<int>}`.
UUID constraints accept canonical or 32-character hexadecimal UUIDs. The router
treats `:itemID` as a literal segment. Read values with
`itemID, ok := mux.PathParam(r, "itemID")`; handle missing parameters safely,
including direct handler calls outside the router. Parse to a typed ID only
when the domain API needs it.

## Middleware Semantics

- `SubRouter` copies the parent's middleware at creation.
- `Use` affects handlers registered afterward; earlier routes stay unchanged.
- Fallback 404, 405, and OPTIONS handlers are created before later `Use` calls
  and are not wrapped by those calls.

Mount public routes before session middleware and protected routes afterward.
For mixed muxes, register public routes first, call `Use`, then register private
routes, or use an appropriately guarded subrouter.

Session middleware may not guarantee current-organization/member context. Where
handlers require it, use a local guard that checks presence and related scope
before calling the handler, returning a centralized HTTP error on failure.
Preserve the API's established scope: the current session usually selects the
organization without an organization path parameter. Pass that scope to database
operations so authorization is enforced by query predicates.

## Verification

Mount a real router for route/middleware tests. Direct handler calls do not
populate parameters or exercise routing. Test changed contracts: exact paths,
constrained IDs, method/Allow behavior, required context, and scope isolation as
relevant. When changing authentication or fallbacks, verify the intended access
and route-disclosure policy for 404, 405, and OPTIONS responses. Reuse existing
coverage; do not add a full route matrix for unrelated handler edits.
