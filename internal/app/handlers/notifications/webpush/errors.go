package webpush

import (
	"net/http"

	"nautilus/internal/errors"
)

func SubscriptionError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to process push subscription", errs...)
}

var ErrEmptyEndpoint = errors.ErrorDetail{
	Message: "endpoint is required",
	Code:    errors.ErrorCodePUSH01,
	Field:   "endpoint",
}

var ErrEmptyAuthKey = errors.ErrorDetail{
	Message: "auth key is required",
	Code:    errors.ErrorCodePUSH02,
	Field:   "keys.auth",
}

var ErrEmptyP256dhKey = errors.ErrorDetail{
	Message: "p256dh key is required",
	Code:    errors.ErrorCodePUSH03,
	Field:   "keys.p256dh",
}
