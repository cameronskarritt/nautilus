package authentication

import (
	"net/http"

	"nautilus/internal/errors"
)

var (
	ErrUnauthorized = errors.NewHTTPError(
		http.StatusUnauthorized,
		"Authentication required",
		errors.ErrorDetail{
			Message: "a valid API key is required",
			Code:    errors.ErrorCodeAPIKEY09,
		},
	)
	ErrInsufficientScope = errors.NewHTTPError(
		http.StatusForbidden,
		"Permission denied",
		errors.ErrorDetail{
			Message: "API key does not grant the required scope",
			Code:    errors.ErrorCodeAPIKEY10,
		},
	)
)
