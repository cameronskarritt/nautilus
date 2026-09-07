# Nautilus

Nautilus is a **business infrastructure service for the agentic era**.

Our hypothesis is that, in the near future, agents will incorporate and operate businesses fully autonomously. Those businesses will still need infrastructure that bridges software and the real world. Nautilus will provide that infrastructure to agents and human operators through a shared service.

## Initial offering: physical mailing addresses

The first service will give customers a physical mailing address and digital access to the mail delivered there. A Nautilus-operated property will be subdivided into customer addresses, each with a unique unit identifier. For example:

```text
123 Main Street Unit #ABC12
```

`ABC12` identifies a customer organization at the property. Organizations are the tenant boundary, so a business can share its address and documents with authorized members and agents.

A human operator, or eventually a machine, will receive physical mail and scan it into an admin portal. Once the scan is available, Nautilus will notify the customer through in-app notifications, webhooks, email, or other configured channels. An agent or human operator will be able to view and edit the document through the web app, API, CLI, or MCP. Editing includes actions such as filling out and signing a form, while preserving the original scan.

Mail content will be treated as highly sensitive, with handling expectations comparable to credentials and passwords: it may contain tax forms and other official correspondence. Nautilus will encrypt content before writing it to blob storage, in addition to storage-provider encryption at rest. Each organization will have its own content encryption key that wraps file-specific data encryption keys, so the bucket contains encrypted bytes rather than plaintext documents.

A separate keyword and embedding search index will support search across an organization's documents. That index will use the selected vendor's encryption capabilities; it is a separate sensitive data store from the encrypted document bucket.

See the [mail service technical design](docs/mail-service-design.md) for the proposed workflows, security boundaries, and decisions still to be made.

## Beyond mail

After the initial mail service, Nautilus will expand into registered agent services, email, telephony, legal services, and other infrastructure needed to operate a business.

## Repository status

The mail service and later offerings described above are planned product capabilities. This repository currently provides the Go application and API foundation, with a React frontend workspace in [`web/`](web/README.md).

### Existing foundation

- Email/password authentication, recovery, verification, TOTP, and SSO
- Personal and shared organizations, membership roles, invitations, and admin assumption
- PostgreSQL migrations, Redis-backed sessions and rate limiting, and audit logs
- Organization-scoped feature flags and API keys for app and API authorization
- Reusable integrations for SES, S3 object storage, and OpenTelemetry
- Docker Compose development services and backend-focused linting and test tooling
- User and admin apps with TanStack Router, TanStack Query, and shared shadcn/Base UI components in a pnpm/Turborepo workspace

## Getting started

Clone the repository:

```bash
git clone git@github.com:cameronskarritt/nautilus.git
cd nautilus
```

## Development

Install the macOS development dependencies:

```bash
./scripts/setup-env
```

Copy the local development configuration, then initialize project-specific encrypted secrets. The first command creates the ignored `.env.keys` file and records the corresponding public key in `.env`:

```bash
cp .env.example .env
dotenvx set ENCRYPTION_KEY "$(openssl rand -hex 32)"
dotenvx set SSO_SIGNING_SECRET "$(openssl rand -hex 32)"
```

Then start the local stack and apply database migrations:

```bash
./scripts/migrate-dev
```

The API is available at `http://localhost:8080/api`. The stack includes the app plus PostgreSQL, Redis, MiniStack, and Garage. Use `./scripts/migrate-dev --reset` to recreate database and MiniStack data; Garage objects persist in their own volumes.

Run the backend checks with:

```bash
dotenvx run -- go test ./...
dotenvx run -- golangci-lint run
```

Start both frontend apps with Node.js 24 and pnpm:

```bash
cd web
pnpm install
pnpm dev
```

The user app runs at `http://localhost:5173` and the admin app at
`http://localhost:5174`. See [`web/README.md`](web/README.md) for workspace
structure, checks, and component commands.

## Object storage

`internal/objectstore.Store` provides `Put`, `Get`, `Delete`, `Head`, `List`, and `Copy`.
The S3 implementation is `internal/objectstore/s3store`; construct it with
`s3store.New(cfg, bucket, usePathStyle)`, passing an AWS SDK configuration.

Compose runs [Garage](https://garagehq.deuxfleurs.fr/documentation/quick-start/)
with automatic single-node and bucket initialization. Start it with
`docker compose up -d garage`. Local development connection values are:

| Setting | Value |
| --- | --- |
| S3 endpoint from the host | `http://localhost:3900` |
| S3 endpoint from the app container | `http://garage:3900` |
| Region | `us-east-1` |
| Bucket | `nautilus-dev` |
| Access key | `GK00000000000000000000000000000000` |
| Secret key | 64 zero characters |
| Path-style addressing | `true` |

Set the SDK configuration's `BaseEndpoint`, `Region`, and `Credentials` to these
values for Garage. For AWS S3, use the standard AWS configuration and leave
`BaseEndpoint` unset. Garage's fixed credentials and single-node configuration
are for local development. MiniStack continues to provide SES with its separate
AWS configuration.

Run the S3 integration test against the local Garage bucket with:

```bash
GARAGE_TEST_ENDPOINT=http://localhost:3900 dotenvx run -- go test ./internal/objectstore/s3store -run TestStore_Garage -count=1
```

## Organization tenancy

Every user belongs to an organization. Standard registration and SSO create a personal organization; shared organizations support member roles and invitations. Admin sessions can assume an organization for support workflows.

GitHub SSO can instead provision a shared organization from GitHub membership. Configure `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`, then set `GITHUB_ORGANIZATION` to require active membership in that organization. GitHub organization admins become owners and other active members become members.

SSO callbacks use this shape:

```text
${API_BASE_URL}/auth/sso/{provider}/callback
```

Google, Microsoft, GitHub, and Apple providers are available when their corresponding environment variables are configured. Store secret values with `dotenvx set`.

Both frontend apps support Google sign-in and protected `/dashboard` routes.
Configure `APP_BASE_URL` and `ADMIN_BASE_URL` as their allowed return origins and
set `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `SSO_SIGNING_SECRET`. See the
[frontend Google sign-in setup](web/README.md#google-sign-in) for the callback URL
and local configuration.

## Tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to an OTLP base endpoint and `TRACEWAY_PROJECT_TOKEN` to its project token. The app sends gzip-compressed traces to `/v1/traces` over OTLP/HTTP.

Use `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` to provide a full trace URL. Set `OTEL_TRACES_ENABLED=true` to use standard exporter variables without a Traceway token.
