package version

import (
	"net/http"

	"nautilus/internal/errors"
)

func Error(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "invalid API version", errs...)
}

var ErrInvalidVersion = errors.ErrorDetail{
	Message: "version string is invalid or unsupported",
	Code:    errors.ErrorCodeAPI01,
}
