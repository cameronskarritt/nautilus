# Nautilus web

Two React 19 + TypeScript apps built with Vite, TanStack Router, and TanStack Query,
managed together with pnpm and Turborepo. Shared shadcn components use **Base UI**
primitives and the `base-nova` style with Tailwind CSS v4.

## Workspace

```text
web/
├── apps/
│   ├── app/       # User-facing app, port 5173
│   └── admin/     # Internal admin dashboard, port 5174
├── packages/
│   ├── api/       # Shared Query options, HTTP calls, and Query client defaults
│   ├── models/    # Framework-independent API response types
│   └── ui/        # Shared shadcn components, utilities, and theme
├── pnpm-workspace.yaml
├── tsconfig.json
├── eslint.config.mjs
└── turbo.json
```

Apps depend on shared packages, never on each other. Shared packages export
TypeScript source, which Vite compiles into each app; they need no separate build
or watch process. Add new shared libraries under `packages/` with a private
`package.json` and declare consumers with `workspace:*` dependencies. Repeated
dependency versions live in the pnpm catalog in `pnpm-workspace.yaml`.

Both apps use the **same Go backend service** through the same `/api` gateway.
User workflows call its app routes; internal administration calls `/api/admin/*`.
They share public routes such as `/api/health`. The frontend split does not require
a second backend service or a separate API origin.

## Development

Use Node.js **24** (see `.node-version`) and **pnpm 10.33.4** (pinned by
`packageManager` in `package.json`). Run these commands from `web/`:

```bash
pnpm install
pnpm dev
```

- User app: <http://localhost:5173>
- Admin app: <http://localhost:5174>

Run one app with `pnpm dev:app` or `pnpm dev:admin`. Both development servers use
strict ports and proxy `/api` to the existing Compose gateway at
`http://localhost:8080` without rewriting the path. The gateway strips `/api`.
Start the backend using the [root development instructions](../README.md#development).
The apps still load with the backend stopped; the service-status page reports
that it is unavailable.

Both apps currently contain an overview, `/status`, and a not-found page. These
are starter shells: login, session routing, and admin screens are not implemented.
The admin app's separate directory is not an authorization boundary; future
admin operations must use the backend's protected `/api/admin/*` endpoints.

## Routes and data

Add file routes under each app's `src/routes/`. The TanStack Router Vite plugin
generates `src/routeTree.gen.ts` during development and builds. Typechecking also
runs the generator, so it works before the first dev server starts. Commit the
generated route tree; do not edit it by hand.

Each app creates its own Query client and passes it to both `QueryClientProvider`
and the router context. The `/status` route prefetches `healthQueryOptions()` and
reads the same cache with `useQuery`. The shared client defaults to a 30-second
stale time and one retry; individual queries can override these settings.

`@workspace/api` calls the existing public `/api/health` endpoint with same-origin
credentials and cancellation support. It checks HTTP errors and validates the
small response contract in `@workspace/models`. Model types follow public HTTP
responses, not the backend's database models. Extend these packages as features
need more API contracts.

## Shared UI

Add shadcn components from either app:

```bash
cd apps/app
pnpm dlx shadcn@latest add dialog
```

The CLI resolves the aliases in `components.json`, writes shared components into
`packages/ui`, and installs their dependencies there. Keep `style: "base-nova"`,
`iconLibrary`, and `baseColor` consistent across both apps and the UI package.
Interactive primitives must use `@base-ui/react`.

```tsx
import { Button } from "@workspace/ui/components/button"
import { cn } from "@workspace/ui/lib/utils"
```

Both apps import `@workspace/ui/globals.css`. Theme tokens live in
`packages/ui/src/styles/globals.css`. Vite scans the current app for Tailwind
classes, and the shared stylesheet explicitly includes UI source. Keep shared components free
of app-specific router imports.

## Checks and builds

```bash
pnpm check         # Formatting, lint, typechecks, API tests, and both builds
pnpm test          # Shared API contract and cancellation tests
pnpm build         # Typecheck and build both apps
pnpm format        # Format workspace files
```

Tests mock HTTP requests and need no running backend.

Build outputs are `apps/app/dist` and `apps/admin/dist`. Deploy each independently,
with a fallback to its `index.html` for client-side routes and same-origin `/api`
requests forwarded to the same Go gateway. `pnpm --filter @workspace/app preview`
and the corresponding admin command preview static builds on ports 6173 and
6174, inheriting the local `/api` proxy. Production hosting must configure that
forwarding separately. This scaffold does not change Caddy or deploy either app.

## References

- [shadcn monorepos](https://ui.shadcn.com/docs/monorepo)
- [shadcn Base UI button](https://ui.shadcn.com/docs/components/base/button)
- [TanStack Router with Vite](https://tanstack.com/router/latest/docs/installation/with-vite)
- [TanStack Router external data loading](https://tanstack.com/router/latest/docs/guide/external-data-loading)
- [Turborepo internal packages](https://turborepo.dev/docs/core-concepts/internal-packages)
