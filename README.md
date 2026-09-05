# Nautilus

Org-enabled Go boilerplate for applications and APIs. It provides a production-shaped backend without prescribing a frontend; there is intentionally no `web/` directory.

## Included

- Email/password authentication, recovery, verification, TOTP, and SSO
- Personal and shared organizations, membership roles, invitations, and admin assumption
- PostgreSQL migrations, Redis-backed sessions and rate limiting, and audit logs
- Organization-scoped feature flags and API keys for app and API authorization
- Provider-neutral LLM clients for OpenAI and Anthropic with trace instrumentation
- Reusable integrations for SES, S3, SQS, Elasticsearch, web push, and OpenTelemetry
- Docker Compose development services and backend-focused linting and test tooling

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

The API is available at `http://localhost:8080/api`. The stack includes the app plus PostgreSQL, Redis, MiniStack, and Elasticsearch. Use `./scripts/migrate-dev --reset` to recreate local data.

Run the backend checks with:

```bash
dotenvx run -- go test ./...
dotenvx run -- golangci-lint run
```

## Organization tenancy

Every user belongs to an organization. Standard registration and SSO create a personal organization; shared organizations support member roles and invitations. Admin sessions can assume an organization for support workflows.

GitHub SSO can instead provision a shared organization from GitHub membership. Configure `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`, then set `GITHUB_ORGANIZATION` to require active membership in that organization. GitHub organization admins become owners and other active members become members.

SSO callbacks use this shape:

```text
${API_BASE_URL}/auth/sso/{provider}/callback
```

Google, Microsoft, GitHub, and Apple providers are available when their corresponding environment variables are configured. Store secret values with `dotenvx set`.

## Tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to an OTLP base endpoint and `TRACEWAY_PROJECT_TOKEN` to its project token. The app sends gzip-compressed traces to `/v1/traces` over OTLP/HTTP.

Use `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` to provide a full trace URL. Set `OTEL_TRACES_ENABLED=true` to use standard exporter variables without a Traceway token.
