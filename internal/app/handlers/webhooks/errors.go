package webhooks

import (
	"net/http"

	"nautilus/internal/errors"
)

var (
	ErrDisabled             = errors.NewHTTPError(http.StatusConflict, "Webhook disabled", errors.ErrorDetail{Message: "enable the webhook before sending a test", Code: errors.ErrorCodeWEBHOOK11})
	ErrWorkflowUnavailable  = errors.NewHTTPError(http.StatusServiceUnavailable, "Webhook tests unavailable", errors.ErrorDetail{Message: "webhook test processing is unavailable", Code: errors.ErrorCodeWEBHOOK12})
	ErrOrganizationRequired = errors.NewHTTPError(http.StatusForbidden, "Organization required", errors.ErrorDetail{Message: "select an organization to access webhooks", Code: errors.ErrorCodeWEBHOOK01})
	ErrForbidden            = errors.NewHTTPError(http.StatusForbidden, "Permission denied", errors.ErrorDetail{Message: "you do not have permission to manage webhooks", Code: errors.ErrorCodeWEBHOOK02})
	ErrName                 = errors.ErrorDetail{Message: "name must contain 1 to 100 characters without control characters", Field: "name", Code: errors.ErrorCodeWEBHOOK03}
	ErrURL                  = errors.ErrorDetail{Message: "url must be a public HTTPS destination without credentials or fragment", Field: "url", Code: errors.ErrorCodeWEBHOOK04}
	ErrEventTypes           = errors.ErrorDetail{Message: "event_types must contain supported event types", Field: "event_types", Code: errors.ErrorCodeWEBHOOK05}
	ErrEmptyUpdate          = errors.ErrorDetail{Message: "provide at least one field to update", Code: errors.ErrorCodeWEBHOOK06}
	ErrEnabled              = errors.ErrorDetail{Message: "enabled must be a boolean", Field: "enabled", Code: errors.ErrorCodeWEBHOOK07}
	ErrCursor               = errors.NewHTTPError(http.StatusBadRequest, "Invalid webhook cursor", errors.ErrorDetail{Message: "cursor is invalid for this list", Field: "cursor", Code: errors.ErrorCodeWEBHOOK08})
	ErrLimit                = errors.NewHTTPError(http.StatusConflict, "Webhook limit reached", errors.ErrorDetail{Message: "an organization can have at most 10 webhooks", Code: errors.ErrorCodeWEBHOOK09})
	ErrRotation             = errors.NewHTTPError(http.StatusConflict, "Secret rotation unavailable", errors.ErrorDetail{Message: "wait for the previous signing secret to expire before rotating again", Code: errors.ErrorCodeWEBHOOK10})
)

func FormError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to process webhook", errs...)
}
