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
- Signed document-availability webhooks with organization settings, delivery history, and Temporal retries; see [Webhooks](docs/webhooks.md)
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

The session app API is available at `http://localhost:8080/api`. The separate
bearer-token API runs at `http://localhost:8081`, with routes such as `/documents`
directly under that URL (no `/api` prefix). Both services rebuild automatically
when Go source files change. To start just the token API and its dependencies,
run `docker compose up -d api` after the initial setup.

The stack includes the app, token API, PostgreSQL, Redis, MiniStack, Temporal,
OpenSearch, and separate upload, webhook, and smoke workers. The setup
provisions a shared user KMS key and application key, verifies the Temporal
namespace, and runs a workflow/activity smoke check. Use
`./scripts/migrate-dev --reset` to recreate database and MiniStack data (including
local S3 objects), Temporal workflow history, and OpenSearch indexes.

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
stops the Compose token API and workers before clearing Temporal history, database,
and MiniStack state, then bootstraps resources and restarts those services. Stop any workers running
directly on your host before a reset. `docker compose down -v` also deletes history
along with the other development volumes. This uses Temporal's
[development server](https://github.com/temporalio/cli#run-a-development-server);
production requires a separately operated Temporal cluster or Temporal Cloud.

`./scripts/migrate-dev` starts all three workers with automatic Go rebuilds and runs the
diagnostic workflow. App, token API, and worker builds use separate temporary directories.
`./scripts/setup-env` verifies Temporal when it is already running; the CLI is
provided by the pinned container image, so no host Temporal installation is needed.
To initialize Temporal on its own, run `bash scripts/temporal/init.sh`.

To run the diagnostic worker from the host instead, stop its Compose service and
start a host worker, then run the diagnostic workflow in another terminal:

```bash
docker compose stop smoke-worker
dotenvx run -- go run ./cmd/worker --queue=smoke
dotenvx run -- go run ./cmd/workflows smoke --queue=smoke
```

Compose runs `worker` on `uploads`, `webhook-worker` on `webhooks`, and
`smoke-worker` on the diagnostic `smoke` queue in the `nautilus` namespace. Start them with:

```bash
docker compose up -d worker webhook-worker smoke-worker
```

The upload worker uses `DATABASE_URL` to finalize document metadata after the
HTTP handler stores the encrypted file in S3. To run it on the host, stop the
Compose `worker` and run `dotenvx run -- go run ./cmd/worker --queue=uploads`.
OCR and document indexing run in the upload workflow; human review will follow. Queues are
created on use and need no namespace bootstrap changes.

Host commands default `TEMPORAL_ADDRESS` and `TEMPORAL_NAMESPACE` to
`localhost:7233` and `nautilus`. Every worker invocation requires `--queue=<name>` and
serves that single registered queue; queue environment variables are no longer
used. `workflows smoke --queue=smoke` submits the diagnostic and waits for its
result. Both smoke submission and registration are restricted to the dedicated
`smoke` queue, so diagnostics cannot enter `uploads` or other application queues.

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

Compose fixes the app and worker address to `temporal:7233` and namespace to
`nautilus`. Worker queues are selected in their Air configurations; host environment
overrides do not change them.

`internal/temporal` owns shared client configuration and worker lifecycle.
Workflow definitions and activities live together in `internal/workflows/<name>`;
the existing diagnostic uses `internal/workflows/smoke/workflow.go` and
`activity.go`. Its `Register` function keeps the workflow and activity names
stable. The standalone command in `cmd/worker` selects registration from a queue
map and passes the configured worker to `internal/temporal`. Unknown or
unimplemented queues fail before connecting to Temporal. `cmd/workflows` owns
workflow submission commands. Queue names are centralized in
`internal/enums/queue.go`; registration maps and workflow helpers use `enums.Queue`.

`internal/workflows/upload` registers `Upload` and its retryable `FinalizeUpload`,
`UploadWebhookDeliveries`, `OCRUpload`, and `IndexUpload` activities on the `uploads`
queue. Finalization atomically marks the document `uploaded` and records its
availability event and matching webhook deliveries after generating and storing
the PDF from source pages. Delivery children start on `webhooks` before OCR;
remote delivery runs independently. The app and separate API service connect to
Temporal for asynchronous webhook tests. See [Webhooks](docs/webhooks.md) for
configuration, signature verification, worker operation, and history retention.
Production client authentication/TLS and deployment configuration remain
separate work.

Upload workflows carry only organization/document IDs. Finalization fetches the ordered
encrypted JPEG/PNG source pages, builds an image PDF in memory, encrypts it, records
its byte size, and marks the document `uploaded`. OCR reads the source images directly
in page order inside an activity, then stores encrypted text output at the stable
private `<document-object-key>/ocr` key with the `document-ocr` encryption purpose.
The worker calls the local olmOCR model through LM Studio. The indexing activity
decrypts the resulting artifact and indexes the filename plus extracted text in
OpenSearch. Each stage
retries independently, and OCR or indexing failures leave the generated PDF
uploaded and downloadable. Deterministic PDF-generation failures mark the document
`failed`; transient storage failures retry. Source page images are retained encrypted. Workflow inputs, activity results, signals, and errors
are retained in Temporal history: keep document bytes, OCR text, filenames, and
secrets out of those payloads. Activities must tolerate retries; workflow code
must remain deterministic. Document-availability webhook publication and dispatch
are implemented; human-review signals and other notification channels remain
future work.

Run the optional server integration test with:

```bash
TEMPORAL_TEST_ADDRESS=localhost:7233 dotenvx run -- go test ./internal/temporal -count=1
```

## OCR

`internal/ocr/lmstudio` implements `ocr.OCR` using LM Studio's OpenAI-compatible
`/chat/completions` endpoint. The upload worker uses `OCR_URL` (default
`http://localhost:1234/v1`), `OCR_MODEL` (default `allenai/olmocr-2-7b`), and optional
`OCR_API_KEY`. Compose connects to `http://host.docker.internal:1234/v1`.

New scan uploads accept only JPEG and PNG page images. OCR processes those source
images directly, without rasterizing the generated download PDF. For existing
documents without source pages, the client retains PDF, PNG/JPEG/WebP, first-frame
GIF, and UTF-8 `text/plain` support (plaintext passes through without inference).
Legacy PDF pages are rendered sequentially; images are scaled to a longest edge of 1288 pixels
with a white background. Requests use the model's official OCR prompt and base64
PNG input. The client validates and strips olmOCR's YAML metadata, then joins
page text in reading order. Tables and equations retain the model's HTML and
LaTeX output. See the [olmOCR input and prompting documentation](https://huggingface.co/allenai/olmOCR-2-7B-1025#usage).

Legacy PDF OCR requires `pdfinfo` and `pdftoppm` from Poppler on the worker PATH.
New scan PDF generation uses the pure-Go `fpdf` library and does not require Poppler.
The development image includes Poppler and DejaVu fonts. Both document input and
rendered pages travel through memory and process pipes; no plaintext temporary
document files are created. The production app image does not run a worker;
any separate worker deployment must provide these rendering dependencies.

New scans are limited to 100 JPEG/PNG pages, 100 MiB combined source bytes,
25 megapixels per page, and a 100 MiB generated PDF. PDF generation also caps
retained image buffers at 256 MiB. PDFs embed full-resolution
images on pages sized at a nominal 300 dpi, retaining aspect ratio and page order;
transparent PNGs are flattened onto white. The PDF has no embedded OCR text layer;
extracted text is a separate encrypted search artifact. Legacy OCR retains its
100 MiB input/text, 100 PDF page, and 100 megapixel image limits. Each model request allows 90 seconds and 4096 output tokens.
Malformed/unsupported documents, pages requiring rotation, and truncated output
fail explicitly without storing partial OCR text. Model and transport failures
retry through Temporal. The OCR activity allows two hours for a full document
and sends heartbeats without document data so cancellation and worker shutdown
can interrupt processing;
workers run at most two activities concurrently, and Compose caps worker memory
at 2 GiB. Generated PDFs (and legacy original documents) remain downloadable if OCR
or indexing fails.

After changing the image or container settings, run
`docker compose up -d --build --no-deps worker`. Previously processed documents
need a new `Upload` workflow run to replace their old OCR artifact and search text.

Run the optional model and full upload tests with:

```bash
LMSTUDIO_OCR_TEST_URL=http://localhost:1234/v1 dotenvx run -- go test ./internal/ocr/lmstudio -run '^TestExtractLive$' -count=1
LMSTUDIO_OCR_TEST_URL=http://localhost:1234/v1 S3_TEST_ENDPOINT=http://localhost:4566 TEMPORAL_TEST_ADDRESS=localhost:7233 OPENSEARCH_TEST_URL=http://localhost:9200 dotenvx run -- go test ./internal/api/handlers/documents -run '^TestUploadMiniStack$' -count=1
```

## Embeddings

`internal/embedding.Embedder` describes a model and embeds batches in input order.
`internal/embedding/lmstudio` implements it with the OpenAI-compatible
`/embeddings` endpoint. Configure its base URL including `/v1`, model ID, and
expected dimensions. The loaded local model is
`text-embedding-qwen3-embedding-4b`, which returns 2560 dimensions. Vectors are
validated and normalized; mismatched models, dimensions, malformed responses,
and empty vectors fail before indexing.

Document text is embedded unchanged. Queries use Qwen's retrieval instruction
format, with an optional `QueryInstruction` override. Requests are limited to
16 inputs, 6000 bytes per input including its instruction, and 96 KiB per batch.
Requests honor cancellation, time out after one minute, reject redirects, and
keep document text and provider response bodies out of errors. See the
[LM Studio embeddings endpoint](https://lmstudio.ai/docs/developer/openai-compat/embeddings)
and [Qwen model instructions](https://huggingface.co/Qwen/Qwen3-Embedding-4B).

Run the optional local-model test with:

```bash
LMSTUDIO_TEST_URL=http://localhost:1234/v1 dotenvx run -- go test ./internal/embedding/lmstudio -count=1
```

## OpenSearch

`internal/search/opensearch` provides a standard-library HTTP client configured by
`Config{URL, Index, Username, Password}`. The intended environment settings are
`OPENSEARCH_URL` (default `http://localhost:9200`), `OPENSEARCH_INDEX` (default
`nautilus-documents-v1`), and optional `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD`.
Credentials are separate from the URL; HTTPS uses normal certificate validation.

The keyword client's `EnsureIndex` creates an explicit strict mapping: `organization_id` and
`document_id` are exact keyword fields, and `text` is analyzed text. Repeated
initialization verifies the existing mapping and rejects incompatible indexes.
Requests have a ten-second timeout, bounded response reads, and sanitized errors.
Index creation is explicit; constructing the client does not contact the server.
See the [OpenSearch index API](https://docs.opensearch.org/latest/api-reference/index-apis/create-index/).

Compose runs the pinned `opensearchproject/opensearch:3.8.0` image as a single node
with a persistent `opensearch-data` volume. Start it with
`docker compose up -d --wait opensearch`. The host URL is
[localhost:9200](http://localhost:9200); the upload worker uses
`http://opensearch:9200`. Its published port binds only to loopback. This local
service disables authentication and TLS, uses a 512 MiB JVM heap, and has a 2 GiB
container memory limit. The smoke worker does not depend on OpenSearch.

`./scripts/migrate-dev` starts OpenSearch and waits for cluster readiness;
`--reset` removes its indexes along with other local data. `docker compose down -v`
also removes its volume. On Linux, OpenSearch requires `vm.max_map_count` of at
least 262144; Docker Desktop needs enough memory for the whole stack. See the
[official Docker setup](https://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/).

The keyword client implements `internal/search.Indexer`. Index and delete operations use
a stable document key containing both organization and document identity and wait
for search visibility. Search uses analyzed keyword matching with an exact
organization filter, returns only document IDs in relevance order, and rejects
partial or timed-out results. Callers must still check PostgreSQL for current
organization access and document availability before returning results.

Keyword search defaults to 50 results and caps requests at 100. Queries are limited to
4 KiB, identifiers to 512 bytes, and indexed text to 101 MiB (including filenames).
Empty queries return no results. Index initialization remains explicit.
The upload worker initializes the configured index before polling Temporal and
passes the client to the indexing activity. Completing a new upload workflow means
its filename and OCR text are searchable through `search.Indexer`; HTTP and UI
search are not exposed yet. Existing workflow histories retain their old behavior
through version markers. Previously uploaded files can be processed by starting
`Upload` with a new workflow ID and the same organization/document IDs.

The search index is a
separate sensitive data store; S3 envelope encryption does not encrypt its terms
or stored text. Production search deployment needs its own access controls, TLS,
and storage encryption.

Run the optional real-engine tests with:

```bash
OPENSEARCH_TEST_URL=http://localhost:9200 dotenvx run -- go test ./internal/search/opensearch -count=1
```

### Vector index

`search.VectorStore` stores document chunks and retrieves keyword and semantic
candidates within an organization. `opensearch.NewVector` accepts an embedding
model identity and creates a separate index with nested text/vector chunks,
Faiss HNSW, and cosine similarity. `EnsureIndex` binds the mapping to the model ID
and dimension count; changing models requires a new index and re-embedding.
It rejects incompatible existing mappings instead of reusing a keyword index.

Each document has at most 128 chunks, each containing at most 4096 bytes of UTF-8
text. Replacement writes the entire document atomically, so shorter replacements
remove old chunks and retries do not duplicate them. Tenant filters apply inside
nearest-neighbor retrieval as well as to final results. Both retrieval methods
return unique document IDs with the best matching chunk for later reranking.
See [OpenSearch nested vector search](https://docs.opensearch.org/latest/vector-search/specialized-operations/nested-search-knn/).

### Hybrid document search

`hybrid.New(store, embedder, reranker)` implements `search.Indexer` and verifies
that the store and embedder use the same model. Indexing splits text into UTF-8
chunks of up to 4096 bytes with about 256 bytes of overlap, embeds batches of 16,
then replaces the entire document after every batch succeeds. The 128-chunk
budget permits about 480 KiB of source text; larger documents fail explicitly
without publishing a partial replacement.

Search retrieves keyword and semantic candidates from the same organization,
then combines their document ranks using equal-weight reciprocal rank fusion:
`score = sum(1 / (60 + rank))`. Duplicate candidates vote once per retrieval
method; ties use document ID order. Each method returns up to 100 candidates,
and the final result contains up to 100 document IDs (50 by default).

Semantic retrieval has a two-second budget covering query embedding and vector
retrieval. An unavailable embedding endpoint, timeout, or invalid semantic
response falls back to keyword results, preserving their rank and requested
limit and skipping the optional reranker. Caller cancellation and deadlines
still return errors. Keyword retrieval errors also remain errors.

An optional `search.Reranker` is composed into the client after fusion. It receives
the query and one bounded passage per candidate and must return each candidate
ID exactly once; invalid rankings fail. Passing nil keeps RRF ordering. A Qwen
reranker adapter is deferred: the local LM Studio endpoints did not reliably
expose both yes/no token scores required by the
[Qwen reranker scoring contract](https://huggingface.co/Qwen/Qwen3-Reranker-0.6B).
No generated yes/no answer is treated as a relevance score.

Run the optional combined Qwen/OpenSearch integration test with:

```bash
LMSTUDIO_TEST_URL=http://localhost:1234/v1 OPENSEARCH_TEST_URL=http://localhost:9200 dotenvx run -- go test ./internal/search/hybrid -run '^TestLiveHybridSearch$' -count=1
```

### Upload indexing configuration

With `EMBEDDING_URL` set, the upload worker initializes the vector index and uses
the hybrid client for filename plus OCR-text indexing. Without it, the worker
keeps the keyword client and `OPENSEARCH_INDEX`. The hybrid configuration uses
`EMBEDDING_MODEL` (default `text-embedding-qwen3-embedding-4b`),
`EMBEDDING_DIMENSIONS` (2560), optional `EMBEDDING_API_KEY` and
`EMBEDDING_QUERY_INSTRUCTION`, and `OPENSEARCH_VECTOR_INDEX` (default
`nautilus-documents-qwen3-4b-v1`). Model changes need a separate vector index and
reprocessing of existing documents. The old keyword index is preserved; writes
go to the selected index, so switching indexes requires backfilling documents.

Compose enables embeddings through the host LM Studio server at
`http://host.docker.internal:1234/v1` with the Qwen model and index above. Keep
that model available in LM Studio. This address works with Docker Desktop;
other container runtimes may need a host gateway mapping or a reachable URL.
Apply changed container settings with `docker compose up -d --no-deps
--force-recreate worker`. Host commands use `http://localhost:1234/v1`.

The indexing activity allows ten minutes for local embedding batches. Service
failures retry without changing the uploaded/downloadable document status.
Indexing still requires successful embeddings; keyword fallback applies only
to search. Documents
exceeding the chunk budget also fail explicitly. Temporal still carries only
organization/document IDs. HTTP and UI search remain separate work.

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
operate on byte slices up to 100 MiB. Each write generates a fresh AES-256 data
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

Documents in an active organization are visible in every upload state. Detail reads return
`{"document": {...}}`; lists return `{"data": [...], "has_more": false}` with
`next_cursor` when another page exists. Metadata contains `id`, `filename`,
`content_type`, `size`, `sha256`, `page_count`, `status`, `created_at`, and `updated_at`. Status is
`uploading`, `uploaded`, or `failed`; object keys and internal IDs remain private.
`sha256` is the lowercase, 64-character hex SHA-256 of the plaintext file returned
by the content endpoint, rather than the encrypted object or source page images.
It is stored atomically when the generated PDF is published and remains unchanged
on retries. Migration backfills existing PDFs from their canonical storage keys;
pending uploads and legacy files without a known hash return an empty string.
Metadata reads do not fetch object bytes or call KMS. Handler responses use `Cache-Control: no-store`.

Lists accept `limit` (default 50, maximum 100) and the opaque `cursor` returned by
the preceding page. Invalid cursors return HTTP 400 with `DOC-03`. Missing and
other-organization document IDs return HTTP 404. Missing or
invalid organization access returns HTTP 403 with `DOC-01` or `DOC-02`; the API's
bearer authentication and scope errors retain their existing `APIKEY` codes.

### Reading document text

`GET /documents/{documentID}/text` returns the worker's extracted text as
`text/plain; charset=utf-8`. It uses the same organization membership or API key
`read` scope as metadata and downloads. Session routes have the `/api` prefix;
the bearer-token API uses the path directly and accepts the same `X-API-Version`
header as metadata. Administrators can also read text through
`GET /api/admin/organizations/{orgID}/documents/{documentID}/text`; this records a
`document_text` audit event containing the document ID.

The response is the complete OCR artifact, including page separators produced by
the worker, with `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
An empty extraction is HTTP 200 with an empty body. Text is decrypted only after
organization access checks, using its organization and document encryption binding.
This read does not start OCR or query the search index.

HTTP 409 with `DOC-12` means text is unavailable: the document is still uploading,
the upload failed, or no OCR artifact exists. `uploaded` means the download is
ready, and does not promise that OCR has completed. An absent artifact cannot
distinguish pending OCR from failed or never-run OCR. Missing or other-organization
documents return HTTP 404. Unconfigured read storage returns HTTP 503 with
`DOC-13`; storage, decryption, and invalid text failures return generic HTTP 500.

```bash
curl --fail-with-body "$API_BASE_URL/documents/$DOCUMENT_ID/text" \
  -H "Authorization: Bearer $API_KEY" \
  -H "X-API-Version: 2026-01-01"
```

### Document uploads

Administrators can use **Upload scans** at `/uploads` in the admin app to select
a recipient, choose or drop images, arrange pages, upload, and download the PDF.

`POST /admin/organizations/{orgID}/documents` accepts `multipart/form-data` with one to 100 JPEG/PNG file parts,
each named `file`. Each part represents one scanned side; multipart order is the
document page order. We control scanner output and require correctly oriented,
individual JPEG/PNG images. PDF, TIFF, GIF, WebP, text, empty, and corrupt inputs are
rejected before persistence. Uploads require an authenticated administrator session;
ordinary members and API keys cannot upload. The user app and API retain document
reads, but their `POST /documents` routes are removed. The admin intake route selects
the active recipient organization by UUID, independently of the assumed organization
or override header, and records identifier-only upload/content-access audit events.

Set `DOCUMENTS_BUCKET` to the destination S3 bucket. The development example uses
`nautilus-dev`, which MiniStack bootstrap creates. The app and API use the shared
AWS configuration and path-style addressing for a configured custom endpoint.
Without a bucket or configured workflow client, uploads return 503 and metadata
reads remain available. With uploads enabled, app startup requires Temporal.
Provision the organization's KMS application key before uploading.

```bash
curl -X POST "$APP_BASE_URL/api/admin/organizations/$ORGANIZATION_ID/documents" \
  --cookie "$ADMIN_COOKIE_JAR" \
  -F 'file=@letter-001.png' \
  -F 'file=@letter-002.jpg'
```

Source images are limited to 100 MiB combined and 25 megapixels each, with up to
64 KiB additional request framing. Uploads stay in bounded memory without plaintext
temporary files. The server validates actual image bytes rather than trusting the
extension or multipart content type. The PDF filename comes from the first image's
basename, replacing its extension with `.pdf` (truncated to 255 characters).

### Seed documents from local scans

Use the seed command to upload the fixtures in `data/filled/image` to an existing
organization through the same admin intake endpoint:

```bash
# Validate and preview the batch without a session or running server.
dotenvx run -- go run ./cmd/app seed --dry-run

# Set NAUTILUS_ADMIN_SESSION to the nautilus-session cookie value from an admin
# sign-in, then upload to the recipient organization.
dotenvx run -- go run ./cmd/app seed --org-id "$ORGANIZATION_ID"
```

The command defaults to `--url http://localhost:8080/api` and
`--dir data/filled/image`. Set `--limit 5` to select only the first five documents,
or use `--help` for all flags. Keep the session token in `NAUTILUS_ADMIN_SESSION`,
not a command-line argument. The organization and its KMS key must already exist;
the app, storage, Temporal, and upload worker must be running to produce PDFs.

Files named `<document>-page-<number>.jpg`, `.jpeg`, or `.png` are grouped and
ordered numerically, starting at page 1 without gaps. Documents are ordered by
basename. Numbered pages take precedence over an unnumbered image with the same
basename; these are treated as alternate renditions, not content deduplication.
Other standalone images become one-page documents. Only the selected directory
is read; PDFs and subdirectories are ignored. The current fixtures select 88
documents containing 213 pages.

All selected images are validated before uploading, using the server's image and
size limits. Uploads run sequentially and print each accepted document ID. The
command stops at the first failure and never retries or follows redirects.
Accepted uploads are processed asynchronously. Rerunning creates new documents;
after an uncertain response, check the organization before repeating the batch.

### Document processing

The server atomically creates the document and its ordered page records, then
stores each source under `<document-object-key>/pages/<1-based-number>`, encrypted
with purpose `document-page` and record identity `<document-UUID>/<page-number>`.
The generated PDF uses purpose `document` and the document UUID as record identity.
S3 receives only envelopes, with content type `application/octet-stream` and no
filename or content metadata. Original page filenames are not retained.

The HTTP 202 metadata has `content_type: application/pdf`, `page_count`, status
`uploading`, and `size: 0` until PDF generation completes. The worker produces and
encrypts the PDF at `<document-object-key>/pdf/<SHA-256-of-PDF>`, then atomically
publishes its private key, byte size, and `uploaded` status. Concurrent retries
cannot replace the published artifact. OCR then reads the source images and indexes
the joined text.
The UI polls while `uploading`; preview and download serve the PDF once `uploaded`.
This status tracks PDF availability, not OCR/indexing completion. Source images
remain private retained processing inputs. Existing documents have `page_count: 0`
and keep their original download and OCR behavior.

Encryption or S3 write failures mark the row `failed` and do not start a workflow.
After all source writes succeed, a workflow-start error returns HTTP 500 and leaves the
row `uploading`, because Temporal may already have accepted the request. Database
failures in an accepted workflow are retried. A crash between S3 storage and
workflow submission can also leave a row `uploading`; automatic reconciliation
and upload idempotency are separate work. A retry currently creates a new document.
No failure path deletes a possibly stored object. Deterministic PDF-generation
failures mark image uploads `failed`; transient worker errors retry. Apply migration
`000009_document_pages.sql` before deploying the new app/API and worker, and deploy
the worker before enabling new image uploads. The worker retains Temporal history
compatibility and legacy document processing. Existing rows default to zero source
pages and are not converted.

Run the optional real S3, Temporal, and OpenSearch upload test against the local stack:

```bash
S3_TEST_ENDPOINT=http://localhost:4566 TEMPORAL_TEST_ADDRESS=localhost:7233 OPENSEARCH_TEST_URL=http://localhost:9200 dotenvx run -- go test ./internal/api/handlers/documents -run '^TestUploadMiniStack$' -count=1
```
