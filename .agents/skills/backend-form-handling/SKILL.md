---
name: backend-form-handling
description: Create and review Go HTTP request forms using the project's httputil.ProcessForm workflow. Use when adding request bodies, normalization, validation, optional JSON fields, or handler form parsing under internal/app/handlers. Implement Normalize and Validate, return project HTTP error details, and route parsing failures through httputil.Error.
---

# Backend Form Handling

## Define the Form

Give every request field a JSON tag. Implement `httputil.Form` with pointer
receivers:

```go
type CreateWidgetForm struct {
	Name  string                    `json:"name"`
	Label optional.Optional[string] `json:"label"`
}

func (form *CreateWidgetForm) Normalize() {
	form.Name = strings.TrimSpace(form.Name)
	if form.Label.Set {
		form.Label.Data = strings.TrimSpace(form.Label.Data)
	}
}

func (form *CreateWidgetForm) Validate() error {
	if form.Name == "" {
		return WidgetError(ErrEmptyName)
	}
	return nil
}

var _ httputil.Form = (*CreateWidgetForm)(nil)
```

Use `optional.Optional[T]` when omission must differ from a supplied zero value.
Normalize optional data only when `Set` is true.

## Normalize Input

Use `Normalize` only for canonical, non-failing cleanup:

- Trim surrounding whitespace from strings.
- Normalize nested request fields when needed.
- Preserve meaningful distinctions such as omitted versus empty optional fields.

Do not perform database lookups or other business operations in `Normalize`.

Embed `httputil.NoopNormalizer` when no normalization is required:

```go
type CompleteRecoveryForm struct {
	httputil.NoopNormalizer
	Token    string `json:"token"`
	Password string `json:"password"`
}
```

## Validate Input

Validate required fields and request-level constraints after normalization.
Return the domain's project error type.

Accumulate independent failures so the client can correct them in one response:

```go
func (form *RegisterForm) Validate() error {
	var errs []error

	if form.Email == "" {
		errs = append(errs, ErrEmptyEmail)
	} else if !validators.ValidateEmail(form.Email) {
		errs = append(errs, ErrInvalidEmail)
	}

	if err := validators.ValidatePassword(form.Password); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return RegistrationError(errs...)
	}
	return nil
}
```

Return immediately when later checks depend on an earlier field or only one
failure can apply.

Embed `httputil.NoopValidator` only when the request truly has no validation:

```go
type FilterForm struct {
	httputil.NoopValidator
	Query string `json:"query"`
}
```

Keep authorization, persistence checks, and other business rules in the handler
or service layer.

## Define Validation Errors

Use one domain error function when several details share a status and top-level
message:

```go
func WidgetError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(
		http.StatusBadRequest,
		"Unable to process widget",
		errs...,
	)
}

var ErrEmptyName = errors.ErrorDetail{
	Message: "name is required",
	Code:    errors.ErrorCodeWIDGET01,
	Field:   "name",
}
```

Register new error codes centrally. Set `Field` to the request's JSON path,
including nested paths such as `keys.auth`.

Use a named `HTTPError` value when the whole response is one fixed case. Do not
wrap a static error in a zero-argument function. Apply the
`backend-http-errors` conventions when adding or changing error definitions.

## Process the Form

Use `httputil.ProcessForm` before business logic:

```go
func (m *Mux) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var form CreateWidgetForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Continue with authorized business operations.
}
```

Do not parse, normalize, or validate the same request separately in the handler.

## Review Checklist

- Give every request field the correct JSON tag.
- Implement both `Normalize` and `Validate`, directly or with a no-op embed.
- Trim all relevant string fields, including optional and nested values.
- Accumulate independent validation failures.
- Return centralized error codes and field-specific details.
- Assert that the form satisfies `httputil.Form`.
- Call `httputil.ProcessForm(r, &form)` before business logic.
- Send processing failures through `httputil.Error` and return.
