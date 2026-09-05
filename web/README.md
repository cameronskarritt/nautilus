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

Both apps contain public overview, `/status`, and `/login` routes, plus a protected
`/dashboard`. The admin app also has `/forbidden` for signed-in users without
administrator access. Administrative API operations remain protected by the Go
backend's `/api/admin/*` middleware.

## Google sign-in

Both apps use the same Google OAuth client and backend cookie session. Configure
a **Web application** OAuth client in Google Cloud with this authorized redirect
URI for local development:

```text
http://localhost:8080/api/auth/sso/google/callback
```

From the repository root, configure the frontend destinations and credentials:

```bash
dotenvx set APP_BASE_URL "http://localhost:5173"
dotenvx set ADMIN_BASE_URL "http://localhost:5174"
dotenvx set GOOGLE_CLIENT_ID "your-client-id"
dotenvx set GOOGLE_CLIENT_SECRET "your-client-secret"
# Set this once if it is not already configured:
dotenvx set SSO_SIGNING_SECRET "$(openssl rand -hex 32)"
```

Restart the backend after changing its environment. The Google button is enabled
when `/api/env` advertises the provider. Complete provider credentials require a
signing secret; without it the backend disables SSO. Never put OAuth secrets in
frontend environment variables. If `GOOGLE_SSO_BASE_URL` overrides `API_BASE_URL`,
register its `/auth/sso/google/callback` URL in Google Cloud instead. When the
OAuth consent app is in testing, add the intended Google accounts as test users.

Sign-in redirects through `/api/auth/sso/google`, and the backend exchanges the
code, sets its HttpOnly session cookie, and returns to the initiating app's
dashboard. The backend only accepts return URLs on the configured
`APP_BASE_URL` and `ADMIN_BASE_URL` origins; scheme and explicit port must match.
Verified-state failures return to that app's login page. Invalid state falls
back to the user app. Use `localhost` consistently in development: the existing
session cookie is scoped to `localhost` and uses Secure cookies. Production
hosting requires configuring cookie scope for its actual hostname.

The pathless `_authenticated.tsx` route checks `/api/users/me` before loading
protected children. Put new authenticated routes under `_authenticated.*`.
Session queries revalidate on navigation and window focus; a 401 returns to
login, while service errors show a retry state. Sign-out ends the backend session,
clears the app's Query cache, and returns to login with a fresh router state.

The admin guard additionally requires `user.admin === true`. Signing in with
Google does not grant administrator privileges; new users remain ordinary users.
Administrator access must already be assigned to the user in the backend before
that account can enter the admin dashboard.

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
pnpm test          # API contracts, cancellation, and protected route tests
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
- [TanStack authenticated routes](https://tanstack.com/router/latest/docs/guide/authenticated-routes)
- [Google web server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server)
- [Turborepo internal packages](https://turborepo.dev/docs/core-concepts/internal-packages)
