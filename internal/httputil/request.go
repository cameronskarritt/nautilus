package httputil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nautilus/internal/errors"
)

// Limit max request bytes
const maxSize int64 = 1 * 1024 * 1024 // 1MB

func parse(rd io.ReadCloser, v any) error {
	rd = http.MaxBytesReader(nil, rd, maxSize)

	dec := json.NewDecoder(rd)
	err := dec.Decode(v)
	if err != nil {
		syntaxErr := new(json.SyntaxError)
		typeErr := new(json.UnmarshalTypeError)
		maxBytesErr := new(http.MaxBytesError)

		switch {
		case errors.As(err, &maxBytesErr):
			return errors.NewHTTPError(
				http.StatusRequestEntityTooLarge,
				"Unable to process request",
				errors.ErrorDetail{
					Message: fmt.Sprintf("request body exceeds max size (%d bytes)", maxBytesErr.Limit),
					Code:    errors.ErrorCodeJSON01,
				},
			)

		case errors.As(err, &syntaxErr):
			return errors.NewHTTPError(
				http.StatusBadRequest,
				"Request body is malformed",
				errors.ErrorDetail{
					Message: syntaxErr.Error(),
					Code:    errors.ErrorCodeJSON02,
				},
			)

		case errors.Is(err, io.EOF):
			return errors.NewHTTPError(
				http.StatusBadRequest,
				"Unable to process request",
				errors.ErrorDetail{
					Message: "request body cannot be empty",
					Code:    errors.ErrorCodeJSON03,
				},
			)

		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.NewHTTPError(
				http.StatusBadRequest,
				"Unable to process request",
				errors.ErrorDetail{
					Message: err.Error(),
					Code:    errors.ErrorCodeJSON04,
				},
			)

		case errors.As(err, &typeErr):
			return errors.NewHTTPError(
				http.StatusBadRequest,
				"Request contains invalid fields",
				errors.ErrorDetail{
					Message: fmt.Sprintf("%s must be %s", typeErr.Field, kindGroup(typeErr.Type.Kind())),
					Code:    errors.ErrorCodeJSON05,
					Field:   typeErr.Field,
				},
			)

		default:
			return errors.Wrap(err, "error parsing request body")
		}
	}

	// More than one JSON object sent
	if dec.More() {
		return errors.NewHTTPError(
			http.StatusBadRequest,
			"Unable to process request",
			errors.ErrorDetail{
				Message: "request body must be a single object",
				Code:    errors.ErrorCodeJSON06,
			},
		)
	}

	return nil
}

func ParseRequestBody(r *http.Request, v any) error {
	if t := r.Header.Get("Content-Type"); t != "" && t != "application/json" {
		return errors.NewHTTPError(
			http.StatusUnsupportedMediaType,
			"Unable to process request",
			errors.ErrorDetail{
				Message: "Content-Type must be application/json",
				Code:    errors.ErrorCodeJSON07,
			},
		)
	}

	return parse(r.Body, v)
}

func ParseBytes(b []byte, v any) error {
	rd := io.NopCloser(bytes.NewReader(b))
	return parse(rd, v)
}

func GetIP(_ context.Context, r *http.Request) (string, error) {
	return r.RemoteAddr, nil
}

func GetXForwardedFor(_ context.Context, r *http.Request) (string, error) {
	header := r.Header.Get("X-Forwarded-For")
	ips := strings.Split(header, ",")
	if len(ips) == 0 {
		return "", nil
	}

	last := strings.TrimSpace(ips[len(ips)-1])
	return last, nil
}

type NoopNormalizer struct{}

func (NoopNormalizer) Normalize() {}

type NoopValidator struct{}

func (NoopValidator) Validate() error { return nil }

type Form interface {
	Normalize()
	Validate() error
}

func ProcessForm[T Form](r *http.Request, form T) error {
	if err := ParseRequestBody(r, form); err != nil {
		return err
	}
	form.Normalize()
	return form.Validate()
}
