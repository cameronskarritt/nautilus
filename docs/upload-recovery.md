# Upload recovery

The uploads worker starts a singleton `UploadRecovery` Temporal workflow. Each
pass it claims at most ten expired `uploading` documents in increasing ID
order, then waits a minute and continues the cursor in a new workflow run. Exhausting the sweep resets
the cursor. Deleted organizations are excluded.

A document has a 15-minute upload lease and a UUID ownership token. The HTTP
handler bounds encryption and object writes to ten minutes. Before starting the
normal deterministic `upload-<organization ID>-<document ID>` workflow, it sets
`upload_ready` with the original token and an unexpired lease. Recovery replaces
the token atomically, so an expired handler cannot start work or release a newer
claim. Recovery completion is fenced by the same token.

A storage error can mean that a PUT succeeded but its response was lost. Handler
cleanup therefore releases its lease for inspection instead of marking the
document failed. A process crash leaves the lease to expire normally. Recovery
first asks Temporal whether the upload workflow exists. Any existing execution,
including a closed execution, retains responsibility for its outcome. Lookup
errors leave the document alone.

If no execution exists, recovery checks every expected source page with HEAD.
All expected pages present means it records readiness and retries normal workflow
startup. This covers the crash between the final PUT and the readiness update.
Missing pages in an unready upload cause a fenced transition to `failed`.
Ready uploads always retry startup; they are never classified as abandoned based
on missing source objects. Transient storage or Temporal errors preserve the
upload for a later attempt. Duplicate starts use the same workflow ID and reject
reuse of an execution still retained by Temporal. Normal finalization decrypts
and validates stored source pages with the organization's KMS key.

The token fences database transitions, not an already in-flight object PUT. A
late PUT from an expired handler may leave an unused encrypted object, but the
handler cannot hand that document off. Recovery does not delete stored objects.
No plaintext is persisted by this mechanism.

Deploy migration 000011 before the new application and uploads worker. Drain old
application upload requests before starting recovery: old binaries do not honor
the new fencing token. Migrated rows receive a fresh 15-minute grace period.
For legacy documents without source-page metadata, recovery checks the original
encrypted document object and retries the existing legacy finalization path.

The sweep runs only while an uploads worker is available. After an accepted
workflow has completed, failed, or been terminated, its normal outcome and
operational retry policy apply; recovery does not override retained executions.
Temporal history retention still bounds duplicate-ID protection, so a ready
upload stranded beyond retention can start a new execution. Finalization remains
idempotent at the database publication boundary.
