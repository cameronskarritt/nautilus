package errors

import (
	"net/http"
)

var (
	ErrNotFound            = httpError(http.StatusNotFound)
	ErrMethodNotAllowed    = httpError(http.StatusMethodNotAllowed)
	ErrTooManyRequests     = httpError(http.StatusTooManyRequests)
	ErrInternalServerError = httpError(http.StatusInternalServerError)
)

type HTTPError struct {
	Status  int     `json:"-"`
	Message string  `json:"message"`
	Errors  []error `json:"errors"`
}

func (e *HTTPError) Error() string {
	return e.Message
}

func NewHTTPError(status int, message string, errs ...error) *HTTPError {
	e := &HTTPError{
		Status:  status,
		Message: message,
	}
	if errs != nil {
		e.Errors = errs
	} else {
		e.Errors = make([]error, 0)
	}

	return e
}

func httpError(status int) *HTTPError {
	return &HTTPError{
		Status:  status,
		Message: "Unable to handle request",
		Errors: []error{
			ErrorDetail{
				Message: http.StatusText(status),
				Code:    httpCode(status),
			},
		},
	}
}

type ErrorDetail struct {
	Message string    `json:"message"`
	Code    ErrorCode `json:"code,omitempty"`
	Field   string    `json:"field,omitempty"`
}

func (err ErrorDetail) Error() string {
	return err.Message
}
