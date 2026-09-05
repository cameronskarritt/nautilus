---
name: backend-http-errors
description: Create and review public error handling for Go HTTP handlers. Use when adding handler errors, validation details, centralized error codes, status mappings, httputil.Error calls, or error payloads for streaming endpoints. Define stable unique codes, use ErrorDetail for client-actionable details, reuse fixed HTTPError values, and keep unexpected internal errors out of responses.
---

# Backend HTTP Errors

## Use the Three Layers

1. Register stable codes in `internal/errors/codes.go`.
2. Use `errors.HTTPError` and `errors.ErrorDetail` for public responses.
3. Define domain responses in each handler package's `errors.go`.

Send ordinary handler failures through `httputil.Error(ctx, w, err)`.

## Register Error Codes

Group codes by domain:

```go
const (
	ErrorCodeWIDGET01 = "WIDGET-01" // Widget name is required
	ErrorCodeWIDGET02 = "WIDGET-02" // Widget name already exists
)
```

Use `ErrorCode<DOMAIN><NN>` with a zero-padded sequence. Give every distinct
client-actionable condition its own code; do not reuse a code for unrelated
messages, fields, or statuses. Preserve existing code meanings because clients
may branch on them.

Do not create a domain code for an unexpected internal failure.
`httputil.Error` converts those failures to the generic `HTTP-500` response.

## Define Shared Validation Envelopes

Use a function when several validation details share one status and top-level
message:

```go
func WidgetError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(
		http.StatusBadRequest,
		"Unable to process widget",
		errs...,
	)
}
```

Define reusable details as values:

```go
var ErrEmptyName = errors.ErrorDetail{
	Message: "name is required",
	Code:    errors.ErrorCodeWIDGET01,
	Field:   "name",
}
```

Set `Field` to the request JSON path for field-specific errors, including
nested paths such as `keys.auth`. Omit it only when the client cannot associate
the condition with one request field.

Accumulate independent validation details and wrap them once:

```go
if len(validationErrors) > 0 {
	return WidgetError(validationErrors...)
}
```

## Define Complete Static Errors

Use a package-level value when status, top-level message, and details are fixed:

```go
var ErrWidgetNameExists = errors.NewHTTPError(
	http.StatusConflict,
	"Widget already exists",
	errors.ErrorDetail{
		Message: "widget name already exists",
		Code:    errors.ErrorCodeWIDGET02,
		Field:   "name",
	},
)
```

Pass the value directly:

```go
if errors.Is(err, widgets.ErrNameExists) {
	httputil.Error(ctx, w, ErrWidgetNameExists)
	return
}
```

Do not wrap a complete static response in a zero-argument constructor.
`NewHTTPError` does not capture a stack, so reconstructing the same response
adds no diagnostic value.

Use generic predefined errors such as `errors.ErrNotFound` when the domain does
not need a distinct public contract.

## Handle Unexpected Errors

Pass unexpected errors through unchanged:

```go
widget, err := widgets.Get(ctx, m.db, id)
if err != nil {
	httputil.Error(ctx, w, err)
	return
}
```

`httputil.Error` logs and traces the original server-side error while returning
the generic internal-server-error payload. Do not send `err.Error()` to the
client or replace the original failure with a new public error before logging
and tracing can inspect it.

## Protect Streaming Responses

Streaming endpoints that can no longer call `httputil.Error` must preserve the
same public/private boundary:

- Log the original error with request context.
- Send a stable public message and code.
- Never place raw `err.Error()` text in SSE, WebSocket, or other stream payloads.
- Keep the stream's error schema consistent and documented.

## Review Handler Flow

Return immediately after every `httputil.Error` call:

```go
if err := httputil.ProcessForm(r, &form); err != nil {
	httputil.Error(ctx, w, err)
	return
}
```

Translate known domain sentinel errors to deliberate public responses. Let
unknown failures reach `httputil.Error` unchanged.

## Review Checklist

- Register each public condition once in `internal/errors/codes.go`.
- Keep every code unique to one stable meaning.
- Use `ErrorDetail` for client-actionable details.
- Set `Field` to the matching JSON path when applicable.
- Use one envelope for details sharing status and message.
- Reuse named complete `HTTPError` values.
- Map known domain sentinels explicitly.
- Pass unknown failures to `httputil.Error` and return.
- Keep raw internal error text out of all response and stream payloads.
