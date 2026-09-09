# Webhooks

Nautilus implements organization-scoped outgoing webhooks for document availability.
This document covers their API, receiver verification, and operating requirements.

## Configure an endpoint

The app router mounts `/webhooks`; the local Caddy proxy exposes it at
`http://localhost:8080/api/webhooks`. App requests use an authenticated session and
the selected organization. Only that organization's owners and admins can access
webhook settings or delivery history. Support/admin organization assumption alone
does not grant this membership permission.

The separate `cmd/api` service mounts `/webhooks` at its own origin. Authenticate
with `Authorization: Bearer <api-key>` and use `X-API-Version: 2026-01-01` (also the
current default). API versions are headers, not URL path segments. The key fixes
the organization; callers cannot select another tenant through a request field.
GET operations require `read`; mutations require `write`. These scopes are
independent: `write` does not imply `read`. API keys do not require a user session.

Paths below are relative to the appropriate service origin; add `/api` when using
the app's development proxy. `webhookID` and `deliveryID` are UUIDs.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/webhooks` | Paginated endpoint list |
| POST | `/webhooks` | `201`, `{ "webhook": ..., "secret": "whsec_..." }` |
| GET | `/webhooks/{webhookID}` | `{ "webhook": ... }` |
| PATCH | `/webhooks/{webhookID}` | Updated endpoint |
| DELETE | `/webhooks/{webhookID}` | `204`; endpoint becomes unavailable |
| POST | `/webhooks/{webhookID}/secret/rotate` | Endpoint and one-time new secret |
| POST | `/webhooks/{webhookID}/test` | `202`, `{ "id": "<delivery UUID>", "status": "pending" }` |
| GET | `/webhooks/{webhookID}/deliveries` | Paginated delivery list |
| GET | `/webhooks/{webhookID}/deliveries/{deliveryID}` | `{ "delivery": ..., "payload": ... }` |
| GET | `/webhooks/{webhookID}/deliveries/{deliveryID}/attempts` | Paginated attempt list |

Create an endpoint with JSON:

```json
{
  "name": "Mail intake",
  "url": "https://receiver.example.com/nautilus",
  "event_types": ["document.available"]
}
```

An organization can have up to 10 nondeleted endpoints, including disabled ones.
Names contain 1–100 characters. Endpoints start enabled. PATCH accepts any nonempty
subset of `name`, `url`, `event_types`, and boolean `enabled`; event types must be a
nonempty list of supported values. Lists return `data`, `has_more`, and optional
`next_cursor`, newest first. Use `limit` (default 50, maximum 100) and the returned
opaque `cursor`; cursors are bound to the organization and list they came from.

Creation and rotation reveal a random 32-byte signing key once, encoded as
`whsec_` plus standard Base64. Save it securely from that response. Subsequent
reads never return signing keys, and webhook responses set `Cache-Control:
no-store`. PostgreSQL stores encrypted signing keys bound to the organization and
endpoint UUID.

Rotation immediately selects the new key and preserves the previous key for 24
hours. During that overlap, outgoing requests contain both signatures; a receiver
can verify with either configured key. Another rotation during the overlap
returns `409`, so the previous key is not displaced early. Update the receiver
during the overlap. Creation and rotation responses are the only opportunities
to read their new raw keys.

Disabling or deleting an endpoint cancels its pending/delivering records. A request
already in flight can still reach the receiver. Re-enabling an endpoint does not
resume canceled deliveries or backfill earlier events.
Deleted endpoints and their history are hidden from these routes.

## Events and document access

`document.available` means the canonical document artifact has been published and
is available through authenticated document endpoints. It occurs before OCR and
search indexing. It does not promise that extracted text or search results are
ready. For scans, the artifact is the generated PDF; legacy documents retain
their original representation.

The document transition, immutable event, and initial matching endpoint deliveries
commit in one PostgreSQL transaction. Recipient selection is frozen for that
occurrence. Retrying finalization preserves its event ID, payload, timestamp, and
recipient set. Upload workflows wait for delivery child workflows to start before
continuing to OCR; they do not wait for remote delivery to finish.

The schema-version-1 payload is intentionally small:

```json
{
  "id": "790e1329-0712-43f2-98ea-2742bbd473d9",
  "type": "document.available",
  "schema_version": 1,
  "occurred_at": "2026-09-09T12:34:56.123456Z",
  "organization_id": "8de9b08f-f3c6-49b9-9316-2c8ca50a5dba",
  "data": {
    "document_id": "397d04e7-ccbe-448a-a908-657e83846772"
  }
}
```

Event, organization, document, endpoint, delivery, and attempt identifiers exposed by these
contracts are UUIDs; the event ID is different from its delivery ID. Payloads
contain no filename, scan, OCR text, storage key, or bearer download URL. Use your
own authorized organization credentials to fetch `/documents/{documentID}` and
`/documents/{documentID}/content` from the API. API document reads require `read`.
Possessing a webhook payload or signing key grants no document access.

`webhook.test` uses the same envelope with empty `data: {}`. POSTing to an enabled
endpoint's `/test` starts durable asynchronous work for that endpoint, regardless
of its subscription list. `202` acknowledges scheduling, not remote acceptance.
The returned delivery UUID can be used to inspect history; its database row may
not be visible immediately while preparation is queued. A disabled endpoint
returns `409`. There is no public replay route. Internal replay workflows exist
for controlled operational use and preserve the original event identity.

## Verify incoming requests

Requests follow the symmetric
[Standard Webhooks specification](https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md).
They use `Content-Type: application/json` and these headers:

| Header | Meaning |
| --- | --- |
| `webhook-id` | Stable event UUID, unchanged across attempts |
| `webhook-timestamp` | Fresh Unix timestamp in seconds for this request |
| `webhook-signature` | One or more space-separated `v1,<Base64 HMAC>` signatures |

Each HMAC uses SHA-256 over `webhook-id + "." + webhook-timestamp + "." + raw_body`.
Decode the secret after removing `whsec_`. Verify the exact received bytes before
parsing JSON; reformatting or serializing parsed JSON changes the signed message.
Accept a matching supported signature, using a constant-time comparison. A
five-minute timestamp tolerance is recommended to limit replay exposure; keep
the receiver clock synchronized. The timestamp is the delivery attempt time,
not the event's `occurred_at`.

This Python standard-library example returns the verified event or raises an
error. Pass the HTTP framework's original body bytes and header mapping:

```python
import base64
import binascii
import hashlib
import hmac
import json
import time


def verify_webhook(headers, raw_body, secret, now=None):
    headers = {name.lower(): value for name, value in headers.items()}
    event_id = headers["webhook-id"]
    timestamp = headers["webhook-timestamp"]
    now = time.time() if now is None else now
    if abs(now - int(timestamp)) > 300:
        raise ValueError("webhook timestamp outside tolerance")
    if not secret.startswith("whsec_"):
        raise ValueError("invalid webhook secret")
    key = base64.b64decode(secret[6:], validate=True)
    if len(key) != 32:
        raise ValueError("invalid webhook secret")
    message = (event_id + "." + timestamp + ".").encode("ascii") + raw_body
    expected = hmac.new(key, message, hashlib.sha256).digest()
    for signature in headers["webhook-signature"].split():
        version, separator, encoded = signature.partition(",")
        if version != "v1" or not separator:
            continue
        try:
            candidate = base64.b64decode(encoded, validate=True)
        except binascii.Error:
            continue
        if hmac.compare_digest(expected, candidate):
            event = json.loads(raw_body)
            if event["id"] != event_id:
                raise ValueError("webhook event identity mismatch")
            return event
    raise ValueError("invalid webhook signature")
```

After verification, durably deduplicate by event ID and enqueue or commit the work
before responding with `2xx`. Treat unknown event types and schema versions
according to your integration's compatibility policy. Do not log signing keys or
document content.

## Delivery behavior and history

Delivery is at least once: a receiver can accept a request before Nautilus records
the outcome, and a retry can deliver it again. There is no ordering guarantee
across events or endpoints. Timestamp verification does not replace event-ID
deduplication.

The workflow sends immediately, then uses exponential backoff from roughly five
seconds with deterministic jitter and a one-hour maximum interval. Automatic HTTP
attempts stop after the delivery's 24-hour window. Every `2xx` is successful; all
other statuses and connection failures remain retryable within that window.
Redirects are not followed. There is no special `Retry-After` scheduling or
automatic endpoint disablement for `410` responses.

Each HTTP request has a 10-second timeout. The client bounds response headers to
16 KiB, drains at most 4 KiB of the body, closes it, and does not retain response
bodies. Attempts record the destination URL, start/finish times, and HTTP status
or a fixed error code such as `timeout`, `network_error`, or
`invalid_destination`. An unfinished attempt means its outcome is unknown, not
that the receiver rejected it. Delivery states are `pending`, `delivering`,
`succeeded`, `failed`, and `canceled`.

Each attempt reloads the current endpoint URL and current signing keys, including
an unexpired previous key. Changing the URL affects subsequent attempts. A
successful attempt and successful terminal status commit atomically. A committed
cancellation cannot be overwritten by a late success. Final database status
persistence continues retrying if storage is unavailable, even after the HTTP
retry window ends; this does not send additional HTTP requests.

Destinations must use HTTPS with normal certificate verification, without URL
credentials or fragments. DNS is checked at connection time and the connection is
pinned to a validated public IP. Private, loopback, link-local, metadata,
multicast, mapped IPv6, and reserved destinations are blocked; mixed public/private
DNS answers are rejected. The sender does not use environment proxies or reuse
connections. Localhost and private development receivers therefore need a public
HTTPS endpoint to receive requests.

## Operate the worker and retention

Run the dedicated `webhooks` worker alongside the `uploads` worker. It needs
`DATABASE_URL`, access to the organization's KMS-backed encryption keys, and the
configured Temporal namespace. It does not need OCR, OpenSearch, or the document
bucket to deliver a webhook.

For the development stack:

```bash
docker compose up -d webhook-worker
```

`./scripts/migrate-dev` also starts it after database setup. To run it from the host
instead:

```bash
docker compose stop webhook-worker
dotenvx run -- go run ./cmd/worker --queue=webhooks
```

Host defaults are `TEMPORAL_ADDRESS=localhost:7233` and
`TEMPORAL_NAMESPACE=nautilus`. The Compose service uses `temporal:7233`. Each
worker invocation serves one queue. The app and separate API service require
Temporal connectivity for asynchronous tests.

Worker startup ensures the singleton `WebhookRetention` workflow is running. It
prunes eligible history immediately, then runs daily and continues as new to
bound its workflow history. Each activity execution processes up to 10 batches
of 1,000 events: at most 10,000 events and their dependent deliveries/attempts.
Retries or a large backlog can extend how long old history remains available.

Events become eligible after 30 days by their database creation time. Events with
any pending or delivering record are pinned, preserving their complete delivery
and attempt history until work becomes terminal. Cleanup rechecks eligibility
under event locks before deleting related rows. The 30-day policy concerns
PostgreSQL webhook history; it does not configure Temporal history retention or
document retention.

Monitor queue backlog, delivery failures, unknown attempt outcomes, and retention
workflow health through the existing worker logs and Temporal tooling. Keep the
upload worker running as well: it commits document events and starts the child
deliveries. A stopped webhook worker leaves work durably queued.

The webhook tables are defined in the fresh database schema
[`internal/database/schema/webhooks.sql`](../internal/database/schema/webhooks.sql).
They are `webhooks`, `webhook_events`, `webhook_deliveries`, and `webhook_attempts`.
This feature intentionally adds no numbered migration: it targets the greenfield
schema. Do not assume `db migrate` upgrades an existing initialized database with
these tables. Recreate disposable development data or arrange an explicit upgrade
before enabling the feature against an older database.
