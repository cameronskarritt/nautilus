package users

import (
	"net/http"

	"nautilus/internal/errors"
)

func FetchUserError(err ...error) error {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to fetch user", err...)
}

func UserUpdateError(err ...error) error {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to update account", err...)
}

var ErrMissingIdentifier = errors.ErrorDetail{
	Message: "request must contain id or username",
	Code:    errors.ErrorCodeUSER01,
}

var ErrUsernameExists = errors.ErrorDetail{
	Message: "username is already taken",
	Code:    errors.ErrorCodeUSER05,
	Field:   "username",
}
