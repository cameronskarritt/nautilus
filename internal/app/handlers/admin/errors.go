package admin

import (
	"net/http"

	"nautilus/internal/errors"
)

func FeatureFlagError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to process feature flag", errs...)
}

var ErrFeatureFlagNotFound = errors.NewHTTPError(
	http.StatusNotFound,
	"Feature flag not found",
	errors.ErrorDetail{
		Message: "feature flag not found",
		Code:    errors.ErrorCodeADMIN05,
	},
)

var ErrOrganizationNotFound = errors.NewHTTPError(
	http.StatusNotFound,
	"Organization not found",
	errors.ErrorDetail{
		Message: "organization not found",
		Code:    errors.ErrorCodeORG01,
	},
)

var ErrEmptyFlagName = errors.ErrorDetail{
	Message: "name is required",
	Code:    errors.ErrorCodeADMIN03,
	Field:   "name",
}

var ErrFlagNameExists = errors.ErrorDetail{
	Message: "feature flag name already exists",
	Code:    errors.ErrorCodeADMIN04,
	Field:   "name",
}
