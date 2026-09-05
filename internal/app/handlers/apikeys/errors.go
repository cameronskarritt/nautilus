package apikeys

import (
	"net/http"

	"nautilus/internal/errors"
)

func APIKeyError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to process API key", errs...)
}

var (
	ErrOrganizationRequired = errors.NewHTTPError(
		http.StatusForbidden,
		"Organization required",
		errors.ErrorDetail{
			Message: "select an organization to manage API keys",
			Code:    errors.ErrorCodeAPIKEY01,
		},
	)
	ErrForbidden = errors.NewHTTPError(
		http.StatusForbidden,
		"Permission denied",
		errors.ErrorDetail{
			Message: "you do not have permission to manage API keys for this organization",
			Code:    errors.ErrorCodeAPIKEY02,
		},
	)
	ErrNameExists = errors.NewHTTPError(
		http.StatusConflict,
		"API key already exists",
		errors.ErrorDetail{
			Message: "an API key with this name already exists",
			Code:    errors.ErrorCodeAPIKEY06,
			Field:   "name",
		},
	)
	ErrNotFound = errors.NewHTTPError(
		http.StatusNotFound,
		"API key not found",
		errors.ErrorDetail{
			Message: "API key not found",
			Code:    errors.ErrorCodeAPIKEY07,
		},
	)
	ErrEmptyName = errors.ErrorDetail{
		Message: "name is required",
		Code:    errors.ErrorCodeAPIKEY03,
		Field:   "name",
	}
	ErrEmptyScopes = errors.ErrorDetail{
		Message: "at least one scope is required",
		Code:    errors.ErrorCodeAPIKEY04,
		Field:   "scopes",
	}
	ErrInvalidScope = errors.ErrorDetail{
		Message: "scope must be read or write",
		Code:    errors.ErrorCodeAPIKEY05,
		Field:   "scopes",
	}
	ErrNameTooLong = errors.ErrorDetail{
		Message: "name must be 100 characters or fewer",
		Code:    errors.ErrorCodeAPIKEY08,
		Field:   "name",
	}
)
