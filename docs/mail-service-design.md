# Physical mail service design

Status: proposed design. This document records the intended technical decisions for Nautilus's first product; it does not describe a completed mail service. The [README](../README.md) describes the business concept and longer-term offerings.

## Scope

Give each customer organization a physical mailing address, receive and scan its mail, notify its operators, and let authorized humans and agents view, search, and edit documents. The web app, API, CLI, and MCP should expose the same organization-scoped capabilities and permissions.

Registered agent services, email hosting, telephony, and legal services are later offerings. Email in this design is a notification channel. Physical forwarding, outbound mailing, and the exact signing workflow remain separate product decisions.

## Addressing and tenant ownership

A Nautilus-operated property will be subdivided into addresses such as `123 Main Street Unit #ABC12`. The unit identifier maps incoming mail to a customer organization. The example is illustrative; the usable postal address format must be established before launch.

- Use the existing organization concept as the tenant boundary, including personal and shared organizations. An agent acts with credentials authorized for an organization.
- Assign a unique customer unit identifier at each property. Keep address assignments and their history so delayed mail can be routed correctly; do not automatically recycle identifiers when an organization closes.
- Store ownership explicitly on mail items, documents, file versions, and processing jobs. A unit identifier or object ID is a routing reference, never authorization.
- Route missing or ambiguous recipient identifiers to a restricted intake workflow. Resolve ownership before customer publication, tenant encryption, indexing, or notification. If an unresolved scan must be retained, it needs a separately encrypted quarantine with explicit operator access and deletion rules.

## Logical records and boundaries

These are conceptual records, not a committed database schema or API contract.

| Record | Responsibility |
| --- | --- |
| Property and address assignment | Physical receiving location, customer unit identifier, organization, and assignment history |
| Mail item | Physical receipt, recipient assignment, intake operator, timestamps, and processing state |
| Document and file version | Organization ownership, immutable original scan, edited versions, and the current version reference |
| Encryption envelope | Object reference, wrapped file key, organization key reference/version, and format information needed to decrypt |
| Processing job | Retryable scan processing, OCR, and indexing tied to a specific organization and file version |
| Notification event and delivery | Durable event identity, destination, attempts, and delivery outcome |
| Audit event | Actor, organization, action, target identifiers, time, and outcome, without document content |

Keep operational metadata in the relational database, encrypted content in object storage, and searchable derivatives in a managed search service. Minimize unencrypted metadata: extracted subjects, sender details, filenames, signatures, and OCR text can reveal the correspondence and need content-level handling. Opaque IDs and routing fields still require access controls.

## Mail lifecycle

1. A human operator receives mail, resolves its customer address, and scans it through the admin portal. A future machine intake path uses the same ownership checks and processing rules.
2. The service accepts the scan over an authenticated, encrypted connection, validates it under defined file type and size limits, and encrypts it before any blob write. Scanner devices and upload infrastructure must avoid persistent plaintext spooling; any necessary temporary persistence must be encrypted and cleaned up.
3. Store the encrypted object and its encryption envelope, then mark the document available only after both the object and its metadata are durable. Preserve the original scan as an immutable version. Reconcile incomplete uploads and orphan objects when storage and database operations fail independently.
4. Commit the availability event with the database state change using a durable outbox or equivalent atomic mechanism. Deliver notifications asynchronously with retries and stable event IDs. Delivery is at least once, so consumers must tolerate duplicates; repeat intake requests must not create duplicate mail items.
5. Run OCR and keyword/embedding indexing asynchronously. Authorized processing workers may decrypt content for this purpose. Index failure must not prevent viewing a successfully stored scan; track indexing status and retry safely.
6. Humans and agents retrieve, search, and edit documents through authorized interfaces. An edit, such as filling or signing a form, creates a new immutable version with a fresh encryption key and records its actor and source version. Detect conflicting edits instead of silently replacing a newer version. Advance the current version and update search only after the new version is durable.

Notifications announce that mail is available, rather than reporting raw physical intake as a readable document. Use minimal payloads such as event ID, organization ID, mail item ID, document ID, and timestamp. Do not include scans, OCR text, sensitive subjects, or bearer download links in email, webhook payloads, or queues. Authenticate webhook deliveries and support replay protection and retry handling; the exact event schema and signing protocol remain to be specified.

## Content encryption

Treat scans, edited files, signatures, OCR output, previews, and other persisted content derivatives as highly sensitive, comparable to credentials or password content. Unlike a password verifier, a document must be recoverable for authorized viewing and processing, so it needs encryption rather than one-way hashing.

### Key hierarchy

Use application-layer envelope encryption in addition to the blob provider's encryption at rest. Envelope encryption encrypts content with a data encryption key (DEK), then wraps that DEK with a key encryption key (KEK). This is an established construction described in the [AWS KMS cryptography documentation](https://docs.aws.amazon.com/kms/latest/developerguide/kms-cryptography.html); that reference does not select AWS as our key-management vendor.

```text
Organization KMS key
    protects the organization's application content key (KEK)
        wraps a fresh file/version data encryption key (DEK)
            encrypts the file bytes before upload to blob storage

Separate shared user KMS key
    protects the shared application key for user secrets such as TOTP
```

- Each organization has its own content key, used as a KEK to wrap its file keys. Do not reuse the application's general encryption secret as the content key for every tenant.
- Generate a fresh DEK for each file version and separately stored derivative. Persist only wrapped DEKs, never plaintext keys. Use one managed KMS key per organization to protect its application content key, and a separate shared managed KMS key for user secrets. Persist application keys only in wrapped form.
- Use a reviewed authenticated encryption construction and library, with correct nonce generation and no nonce reuse under a key. Authenticate the organization ID, document ID, file version ID, and format version as encryption context so ciphertext cannot be silently substituted between records. Exact algorithms and framing, including any streaming format, must be selected before implementation.
- Persist versioned envelope metadata sufficient to locate the correct KEK version and decrypt the file: wrapped DEK, algorithm/format version, nonce and authentication information, and object reference. Nonsecret framing may live with the ciphertext; never include content in object names, tags, or encryption context.
- Keep provider encryption at rest enabled as an additional layer. Blob uploads, copies, multipart parts, previews, and backups must contain application-encrypted content; there must be no plaintext staging bucket or upload path.

### Key management boundary

[`kms.KeyManager`](../internal/kms/kms.go) is the initial provider-independent boundary:

```go
type KeyManager interface {
    OrganizationKey(ctx context.Context, orgID string) ([]byte, error)
    UserKey(ctx context.Context) ([]byte, error)
}
```

`OrganizationKey` accepts the organization's external ID and returns its application encryption key. `UserKey` returns one shared application key for user-owned secrets such as TOTP, independent of the active organization. This removes the need to bind user authentication secrets to personal organizations. The user key must be distinct from all organization keys; it is never a fallback for a missing organization key.

Successful lookups return caller-owned copies of stable, raw 32-byte keys, not hex/base64 strings or provider key identifiers. A KMS-backed implementation unwraps these application keys using the corresponding managed KMS key; the managed KMS key itself is not exported. Callers authorize the operation before requesting a key. The provider must report a failure instead of returning missing or invalid key material as a successful lookup.

The first PR adds only this interface and its contract. Provider implementation, wrapped-key persistence and provisioning, middleware context wiring, and migration of existing TOTP ciphertext follow separately. Organization middleware will resolve the authenticated tenant before attaching its encryptor/decryptor; authentication flows will use the shared user key, including login before a session exists. The existing `ENCRYPTION_KEY` remains in use until its ciphertext has been migrated and its runtime dependency can be removed safely.

This initial interface does not select historical key versions. Application-key replacement and version-aware lookup must be designed before changing returned key material; repeated lookups and restarts must not silently generate replacement keys that make existing ciphertext unreadable.

### Read path and trust boundary

Authenticate the actor and authorize access to the requested organization and document before retrieving an envelope or invoking key unwrapping. Resolve key references from trusted ownership records rather than accepting caller-supplied keys or tenant identifiers as authority. Fetch ciphertext, unwrap the DEK through the approved key-management path, verify authenticity, and deliver plaintext only through an authorized encrypted response. Fail closed on missing keys, ownership mismatches, or authentication failures.

The service and its approved processing workers necessarily handle plaintext while scanning, viewing, editing, OCR, and indexing. This is not end-to-end encryption that hides content from Nautilus. The design protects stored document bytes from blob-store exposure, including a bucket read that transparently removes provider encryption. It does not protect content from a compromised process that has both document and key access, an authorized recipient's device, or the physical mail intake operator.

Limit plaintext and unwrapped-key lifetimes to the work that needs them. Disable document-body logging, request/response capture, and shared plaintext caches. Apply the same restrictions to traces, error reports, job payloads, temporary files, and third-party processing integrations. Use synthetic documents in development and tests. Audit access using identifiers and outcomes only.

### Rotation, recovery, and deletion

Version organization keys and retain the key references needed by existing files. Routine KEK rotation can rewrap DEKs without rewriting file ciphertext. If a DEK or plaintext content is exposed, merely rewrapping that DEK does not repair the exposure; affected files need new DEKs and re-encryption as part of incident handling.

Backup and restore procedures must preserve the relationships among organizations, file versions, wrapped DEKs, and recoverable KEKs. Test recovery before accepting customer content. Key access must use restricted service identities and produce audit records.

Deletion must revoke serving access promptly and remove all file versions, derivatives, search entries, and pending processing work according to a defined retention policy. Workers must recheck deletion state before publishing results so a retry cannot restore deleted content. Object versions, backups, scanner copies, and retained keys must be included in that policy. Destroying an organization key is irreversible and does not erase independently encrypted search data or previously downloaded plaintext.

## Search is a separate sensitive data store

Build a managed index over extracted text and embeddings to support both keyword and semantic search within an organization. Document versions in encrypted blob storage remain the source of truth; the index is derived and rebuildable.

Use the search vendor's encryption capabilities instead of implementing custom searchable encryption. The index is outside the file envelope-encryption boundary: text, tokens, embeddings, snippets, and query logs can expose information about the documents. Application-encrypted blobs alone do not protect that data.

- Require encryption in transit and vendor-supported encryption at rest. Select the vendor's tenant isolation and key-management configuration before sending customer data; do not assume it has the same per-organization keys as the blob design.
- Enforce organization scope inside every search request, including keyword/vector retrieval, counts, facets, and snippets. Apply any document-level permissions as well. Revalidate returned document/version IDs against authoritative access and deletion state before displaying results; filtering leaked results afterward is not an isolation strategy.
- Restrict indexing identities and send only the content and identifiers needed for retrieval. OCR and embedding providers, if separate from search, are additional plaintext recipients whose retention, training use, access, and deletion behavior must be settled before selection.
- Index committed versions only. Use version-aware, idempotent updates so retries cannot replace newer content with older content. The initial search behavior should target current document versions; historical-version search remains a product decision.
- Propagate edits and deletions, track index lag, and suppress stale or deleted results. Ensure vendor backups and logs are covered by retention and deletion decisions. Avoid logging search text in Nautilus telemetry.

## Authorization across interfaces

The web app, API, CLI, and MCP must share the same server-enforced organization and document authorization. Give agents scoped credentials and distinguish reading, editing, and managing notification destinations. Key possession is not the application authorization model, and tenant identifiers supplied by clients cannot bypass ownership checks.

Admin intake requires permission to receive and assign mail. The existing ability to assume an organization for support must not silently grant unrestricted content access: define explicit content-access permissions and audit operator reads, edits, downloads, recipient corrections, and key operations. Preserve the association between a machine credential and its organization in audit records.

Treat document text as untrusted input. It can contain instructions addressed to an agent; OCR, search results, and MCP responses must remain document data and must not grant authority to execute actions. Signing requires an explicit authorized edit operation. What constitutes a signature, who may sign, and any external signing provider remain open decisions.

## Existing foundation and implementation gaps

The repository already provides organizations, membership and authentication, API keys, admin/user frontends, audit logging, SES integration, and an S3-compatible object storage interface. These can support the service but do not establish the mail-content security boundary by themselves.

The current encryption helper uses a single application `ENCRYPTION_KEY` for secrets such as MFA configuration; it does not implement tenant/file envelope encryption. The object store writes the bytes supplied by its caller, so content encryption must happen before calling it. Existing API key scopes are general `read` and `write` scopes. An organization-scoped outbox schema exists, but mail event production and delivery still need implementation. The existing `internal/mail/` package sends transactional email; it does not receive physical mail.

Mail intake, address assignments, document/version records, per-organization key management, encrypted file processing, durable mail notifications, OCR/search integration, document editing, CLI, and MCP remain planned work. Review existing general-purpose logging, encryption, storage, and admin assumptions before using them for mail content.

## Decisions required before launch

- Confirm the property's deliverable address format, customer onboarding, recipient verification, physical custody, and handling of unknown recipients and closed accounts.
- Select blob storage, managed key management, encryption format/library, file limits, and the handling of large uploads and temporary processing data.
- Select OCR, embedding, and search vendors and their encryption, isolation, retention, residency, and deletion configurations.
- Define content-access roles, agent scopes, webhook delivery contracts, edit/signing semantics, and whether customers can retrieve historical versions.
- Set retention and recovery policies for physical mail, originals, edits, indexes, backups, keys, and intake devices; decide whether forwarding or disposal is part of the initial service.

Before launch, verify tenant isolation across every interface and worker; confirm that raw blob reads contain encrypted content only; exercise tamper rejection, key rotation and recovery, duplicate intake/delivery, conflicting edits, index failures, and deletion with queued retries. Verify that logs, traces, notifications, and temporary storage do not retain document content. These are future implementation acceptance criteria, not tests completed by this documentation change.
