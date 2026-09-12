---
name: backend-http-errors
description: Define or change public Go HTTP errors using the repository error codes, HTTPError envelopes, and httputil.Error handler.
---

# Public HTTP Errors

Project error contracts live in `internal/errors/codes.go`,
`internal/errors/http.go`, and each handler package's `errors.go`.

- Register stable `ErrorCode<DOMAIN><NN>` constants such as `WIDGET-01` centrally.
  Preserve existing meanings; distinct client-actionable conditions need distinct
  codes. Unexpected server failures use the generic `HTTP-500` contract.
- Use `errors.ErrorDetail` for public details. `Field` is the request JSON path,
  including nested paths such as `keys.auth`; omit it for non-field errors.
- Use one function accepting `errs ...error` when multiple validation details
  share an HTTP status and message. Accumulate independent details and wrap once.
- Use a named package-level `errors.NewHTTPError(...)` value for a complete fixed
  response. The constructor captures no stack, so a zero-argument wrapper adds
  no diagnostics. Treat shared response values as immutable.
- Reuse generic values such as `errors.ErrNotFound` where no domain-specific
  public contract is needed.

Translate known domain sentinels into the deliberate public response. Pass
unknown failures to `httputil.Error(ctx, w, err)` unchanged and return immediately.
It logs and traces the original failure while returning the generic server error.
Do not replace that failure before the handler can observe it or expose its
`err.Error()` text to the client.

After stream headers are sent, preserve the same boundary: log the original
failure with request context and send the stream's stable public message/code.
Do not send raw internal errors in SSE or WebSocket payloads.

Tests for changed public errors should assert the status, code, and applicable
field, rather than internal error wording.
