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
dotenvx set SSO_SIGNING_SECRET "$(openssl rand -hex 32)"
```

Then start the local stack and apply database migrations:

```bash
./scripts/migrate-dev
```

The API is available at `http://localhost:8080/api`. The stack includes the app,
PostgreSQL, Redis, MiniStack, Temporal, and a separate workflow worker. The setup
provisions a shared user KMS key and application key, verifies the Temporal
namespace, and runs a workflow/activity smoke check. Use
`./scripts/migrate-dev --reset` to recreate database and MiniStack data (including
local S3 objects) and clear Temporal workflow history.

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

## Temporal

Compose includes Temporal's development server with a persistent SQLite database
in `temporal-data`. Start it with:

```bash
docker compose up -d --wait temporal
```

The gRPC endpoint is `localhost:7233` from the host and `temporal:7233` from
Compose containers. Open the UI at [localhost:8233](http://localhost:8233).
The server creates the `nautilus` namespace on startup. Both published ports bind
to loopback because this local server has no authentication.

Workflow history survives container recreation. `./scripts/migrate-dev --reset`
stops the Compose worker before clearing Temporal history, database, and MiniStack
state, then bootstraps resources and restarts the worker. Stop any workers running
directly on your host before a reset. `docker compose down -v` also deletes history
along with the other development volumes. This uses Temporal's
[development server](https://github.com/temporalio/cli#run-a-development-server);
production requires a separately operated Temporal cluster or Temporal Cloud.

`./scripts/migrate-dev` starts the worker with automatic Go rebuilds and runs the
diagnostic workflow. App and worker builds use separate temporary directories.
`./scripts/setup-env` verifies Temporal when it is already running; the CLI is
provided by the pinned container image, so no host Temporal installation is needed.
To initialize Temporal on its own, run `bash scripts/temporal/init.sh`.

To run a worker from the host instead, stop the Compose worker and start a host
worker, then run the diagnostic workflow in another terminal:

```bash
docker compose stop worker
dotenvx run -- go run ./cmd/worker --queue=uploads
dotenvx run -- go run ./cmd/worker --queue=uploads --smoke
```

Compose runs one `worker` process pinned to the `uploads` queue in the `nautilus`
namespace. Start it with:

```bash
docker compose up -d worker
```

OCR and indexing will be activities of the upload workflow, using this same queue.
Human review will be coordinated within that workflow. OCR and indexing do not
have separate queues or worker services. Queues are created on use and need no
namespace bootstrap changes.

Host commands default `TEMPORAL_ADDRESS` and `TEMPORAL_NAMESPACE` to
`localhost:7233` and `nautilus`. Every invocation requires `--queue=<name>` and
serves that single queue; queue environment variables are no longer used. Add
`--smoke` to run a diagnostic workflow on the selected queue and exit.

The smoke check starts one unique workflow and waits directly for its activity
result, with a ten-second connection deadline and a one-minute execution wait.
The worker handles SIGINT/SIGTERM with a 30-second activity grace period.
A startup or fatal error fails the process. The shared worker lifecycle allows
35 seconds to stop after cancellation or failure; the
command allows 40 seconds after a shutdown signal, and a second signal forces
an immediate exit with an error.

Command execution and worker-loop panics are recovered, logged with a stack trace,
and fail the process. Temporal handles activity
panics through activity retries. Workflow panics fail the current workflow task
and keep the workflow open for a code fix (`BlockWorkflow`).

Compose fixes the worker address to `temporal:7233`, namespace to `nautilus`, and
queue to `uploads`, matching local bootstrap and smoke checks. Host environment
overrides do not change the Compose worker's queue. The worker currently registers
the diagnostic workflow and activity; upload processing is the next consumer.

`internal/temporal` owns shared client configuration and worker lifecycle.
Workflow definitions and activities live together in `internal/workflows/<name>`;
the existing diagnostic uses `internal/workflows/smoke/workflow.go` and
`activity.go`. Its `Register` function keeps the workflow and activity names
stable. The standalone command in `cmd/worker` registers workflows on the selected
queue, then passes the configured worker to `internal/temporal` to run it.

Future upload processing belongs in `internal/workflows/upload`, with
`workflow.go`, `ocr.go`, and `index.go` holding the workflow and its activities,
all served by the `uploads` queue.
The HTTP app does not connect to Temporal until it has a workflow consumer.
Production client authentication/TLS and deployment configuration remain
separate work.

Future upload workflows should carry opaque organization/document IDs and fetch
content inside activities. Workflow inputs, activity results, signals, and errors
are retained in Temporal history: keep document bytes, OCR text, filenames, and
secrets out of those payloads. Activities must tolerate retries; workflow code
must remain deterministic. OCR, indexing, human-review signals, and reliable
dispatch from database changes are not implemented by this foundation.

Run the optional server integration test with:

```bash
TEMPORAL_TEST_ADDRESS=localhost:7233 dotenvx run -- go test ./internal/temporal -count=1
```

## Object storage

`internal/objectstore.Store` provides `Put`, `Get`, `Delete`, `Head`, `List`, and `Copy`.
The S3 implementation is `internal/objectstore/s3store`; construct it with
`s3store.New(cfg, bucket, usePathStyle)`, passing an AWS SDK configuration.

Compose runs MiniStack for S3, SES, and KMS. Start it and initialize the
local resources with:

```bash
docker compose up -d ministack
bash scripts/ministack/init.sh
```

Local development connection values are:

| Setting | Value |
| --- | --- |
| S3 endpoint from the host | `http://localhost:4566` |
| S3 endpoint from the app container | `http://ministack:4566` |
| Region | `us-east-1` |
| Bucket | `nautilus-dev` |
| Access key | `test` |
| Secret key | `test` |
| Path-style addressing | `true` |

Use the shared `internal/aws.LoadConfig` configuration from the app container,
with bucket `nautilus-dev` and path-style addressing enabled. For AWS S3, use
the standard AWS configuration and leave `BaseEndpoint` unset. MiniStack's
fixed credentials are for local development.

Bucket state and object bytes persist in the `ministack-data` volume across
container recreation. `./scripts/migrate-dev --reset` removes that volume,
including S3 objects. Existing Garage volumes are not migrated or deleted by
this change; retain them if they contain local data you need.

Run the S3 integration test against the initialized local bucket with:

```bash
S3_TEST_ENDPOINT=http://localhost:4566 dotenvx run -- go test ./internal/objectstore/s3store -run '^TestStore_MiniStack$' -count=1
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

## Key management

`internal/kms/awskms` implements `kms.KeyManager` using AWS KMS and the database's
immutable `kms_keys` records. Each organization has a separate managed KMS key;
user secrets share a distinct managed KMS key. Only wrapped application keys and
canonical KMS key ARNs are stored in the database. Key lookups decrypt the stored
application key and never provision or replace it.

Create the managed KMS keys through your infrastructure tooling, then apply the
database migrations and provision their application keys. The following commands
use exported `USER_KMS_KEY_ARN`, `ORG_KMS_KEY_ARN`, and `ORG_ID` variables containing
canonical key ARNs and an existing organization's external UUID. Aliases are not
accepted because they can be reassigned.

Generate the shared user application key before accepting MFA traffic:

```bash
dotenvx run -- go run ./cmd/app keys provision-user --key-arn "$USER_KMS_KEY_ARN"
```

Provision an organization's key with:

```bash
dotenvx run -- go run ./cmd/app keys provision-organization --org-id "$ORG_ID" --key-arn "$ORG_KMS_KEY_ARN"
```

Provisioning requires `kms:GenerateDataKeyWithoutPlaintext`; lookups require
`kms:Decrypt`. Grant access
only to the relevant managed keys. Do not enable SDK request/response body logging
for key operations. These commands do not create or delete managed KMS keys.

Authentication now receives a lazy shared-user encryptor through its router's
middleware, including login before a session exists. Organization routes receive
an encryptor bound to the authenticated organization's external ID. Missing or
unauthorized organization context has no encryptor; admin organization assumption
alone does not grant content access. API keys resolve their active organization
from the database before that binding is created.

Key lookups occur only when encryption or decryption is used, carry request
cancellation, and have a ten-second deadline. Missing keys and provider failures
fail closed. All encryption uses scoped KMS keys; there is no environment-key
configuration, import command, or legacy ciphertext reader.

`Encrypter.Seal(ctx, plaintext, binding)` and `Open(ctx, envelope, binding)`
operate on byte slices up to 16 MiB. Each write generates a fresh AES-256 data
key and wraps it with the scoped application key. The versioned envelope
authenticates its framing, scope, purpose, and immutable record identity.
`encrypt.Binding` requires `Purpose` and `RecordID` from trusted application
state. TOTP uses the shared user scope, purpose `totp`, and `user:<internal ID>`;
copying its ciphertext to another user fails authentication.

The object-store adapter accepts arbitrary bytes. Document uploads seal content
with purpose `document` and the immutable document UUID as the record identity.
Streaming files and document downloads are not implemented yet.
Application keys remain stable; replacing them requires a separate versioned-key
design. Do not replace persisted key records to simulate rotation.

Request on-demand rotation of the managed KMS key's backing material with:

```bash
dotenvx run -- go run ./cmd/app keys rotate-user
dotenvx run -- go run ./cmd/app keys rotate-organization --org-id "$ORG_ID"
```

Rotation resolves the canonical ARN from the trusted registry; it accepts no
replacement ARN and writes no database records. The managed KMS key, wrapped
application key, and application ciphertext remain fixed. AWS retains previous
backing material to decrypt old wrapped keys. This does not rotate a compromised
application key. See [AWS key rotation](https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html).

Grant the operator `kms:RotateKeyOnDemand` on the intended managed keys. A successful
command means the rotation was requested, not completed. Check `GetKeyRotationStatus`
and `ListKeyRotations` through AWS before declaring completion or retrying an
ambiguous failure; the command deliberately disables automatic SDK retries.
Unsupported providers return an error. Enable automatic rotation through your
infrastructure using `EnableKeyRotation` if desired; it does not require application
changes. See the [on-demand rotation API](https://docs.aws.amazon.com/kms/latest/APIReference/API_RotateKeyOnDemand.html)
and [automatic rotation API](https://docs.aws.amazon.com/kms/latest/APIReference/API_EnableKeyRotation.html).

Back up `kms_keys` together with organization identities and encrypted data. Test
restores in an isolated database using the exact canonical ARNs, wrapped key blobs,
and organization external UUIDs. Retain the original KMS keys and decrypt permission;
a database backup cannot recover a deleted provider key. Restoring a backup must
not run provisioning or create replacement key material.

Run the optional SDK smoke test against a local MiniStack instance with:

```bash
KMS_TEST_ENDPOINT=http://localhost:4566 dotenvx run -- go test ./internal/kms/awskms -run '^TestManagerMiniStack$' -count=1
```

MiniStack emulates KMS cryptography; this checks fresh provisioning and envelope
recovery after restoring wrapped records. Provider tests verify rotation scope,
unchanged records, and failure without automatic retries. Actual AWS backing-material
rotation is not established by the emulator; verify completion through AWS.

## Tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to an OTLP base endpoint and `TRACEWAY_PROJECT_TOKEN` to its project token. The app sends gzip-compressed traces to `/v1/traces` over OTLP/HTTP.

Use `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` to provide a full trace URL. Set `OTEL_TRACES_ENABLED=true` to use standard exporter variables without a Traceway token.

## Document metadata

Both the session app and bearer-token API expose `GET /documents` and
`GET /documents/{documentID}` for the current organization. Session users need
an actual organization membership; viewers can read metadata. An admin's assumed
organization alone grants no document access. API keys require the `read` scope;
the API supports `X-API-Version: 2026-01-01` and defaults to that version.

Only ready documents in an active organization are visible. Detail reads return
`{"document": {...}}`; lists return `{"data": [...], "has_more": false}` with
`next_cursor` when another page exists. Metadata contains `id`, `filename`,
`content_type`, `size`, `created_at`, and `updated_at`; object keys, internal IDs,
and processing state remain private. Metadata reads do not fetch object bytes
or call KMS. Handler responses use `Cache-Control: no-store`.

Lists accept `limit` (default 50, maximum 100) and the opaque `cursor` returned by
the preceding page. Invalid cursors return HTTP 400 with `DOC-03`. Pending,
missing, and other-organization document IDs all return HTTP 404. Missing or
invalid organization access returns HTTP 403 with `DOC-01` or `DOC-02`; the API's
bearer authentication and scope errors retain their existing `APIKEY` codes.

### Document uploads

`POST /documents` accepts `multipart/form-data` with exactly one part named
`file`. Session owners, admins, and members can upload; viewers can read metadata.
API keys need `write` scope to upload and `read` scope to read metadata. The
organization comes from authenticated context; admin organization assumption
alone does not grant access.

Set `DOCUMENTS_BUCKET` to the destination S3 bucket. The development example uses
`nautilus-dev`, which MiniStack bootstrap creates. The app and API use the shared
AWS configuration and path-style addressing for a configured custom endpoint.
Without a bucket, uploads return 503 and metadata reads remain available.
Provision the organization's KMS application key before uploading.

```bash
curl -X POST "$API_BASE_URL/documents" \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "X-API-Version: 2026-01-01" \
  -F 'file=@letter.pdf'
```

Files are limited to 16 MiB, with up to 64 KiB additional request framing. Uploads
stay in bounded memory without plaintext temporary files. The server detects the
content type, generates a private UUID object key, and encrypts with purpose
`document` and the document UUID as record identity. S3 receives only the envelope,
with content type `application/octet-stream` and no filename or content metadata.

A metadata row starts pending and becomes ready only after the encrypted object
write succeeds. Failures retain the pending row and any object so later
reconciliation can resolve ambiguous writes. Pending rows are hidden from reads;
a failed finalization never triggers deletion of a possibly published object.
Automatic reconciliation, upload idempotency, file downloads, and document editing
are separate work. A retry currently creates a new document.

Run the optional real S3 upload and metadata isolation test against local MiniStack:

```bash
S3_TEST_ENDPOINT=http://localhost:4566 dotenvx run -- go test ./internal/api/handlers/documents -run '^TestUploadMiniStack$' -count=1
```
