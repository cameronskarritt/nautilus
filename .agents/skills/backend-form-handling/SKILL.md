---
name: backend-form-handling
description: Implement request forms using the repository httputil.ProcessForm API when adding or changing Go HTTP request bodies.
---

# Request Forms

`internal/httputil/request.go` defines `Form` with `Normalize()` and
`Validate() error`. `httputil.ProcessForm(r, &form)` parses the body, normalizes,
then validates; its generic constraint checks interface satisfaction.

Give request fields JSON tags. Use pointer receivers and embed
`httputil.NoopNormalizer` or `httputil.NoopValidator` when that operation has no
work. Use `optional.Optional[T]` when omission differs from a supplied zero
value; normalize its `Data` only when `Set` is true.

Normalize canonical, non-failing cleanup only. Trim fields whose semantics allow
it; preserve passwords and other whitespace-sensitive values. Keep persistence,
authorization, and business operations outside normalization and request-level
validation.

Accumulate independent validation failures into the domain's error envelope.
Return early for dependent checks. Follow `backend-http-errors` when defining
public errors; field details use the request's JSON path.

```go
var form CreateWidgetForm
if err := httputil.ProcessForm(r, &form); err != nil {
    httputil.Error(r.Context(), w, err)
    return
}
```

Do not separately repeat parsing, normalization, or validation in the handler.
Use existing form tests for the changed normalization and validation contract.
