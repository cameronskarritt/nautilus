package agent

import (
	"net/http"

	"nautilus/internal/errors"
)

func StreamError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Invalid stream request", errs...)
}

func ApprovalError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Invalid approval request", errs...)
}

var ErrApprovalNotFound = errors.NewHTTPError(
	http.StatusNotFound,
	"Approval not found",
	errors.ErrorDetail{
		Message: "approval not found",
		Code:    errors.ErrorCodeAPPROVAL01,
	},
)

var ErrApprovalNotPending = errors.NewHTTPError(
	http.StatusConflict,
	"Approval already resolved",
	errors.ErrorDetail{
		Message: "approval is not in pending status",
		Code:    errors.ErrorCodeAPPROVAL02,
	},
)

var ErrOrganizationRequired = errors.NewHTTPError(
	http.StatusBadRequest,
	"Organization context required",
	errors.ErrorDetail{
		Message: "organization context is required",
		Code:    errors.ErrorCodeAGENT02,
	},
)

var ErrStreamNotFound = errors.NewHTTPError(
	http.StatusNotFound,
	"Agent stream not found",
	errors.ErrorDetail{
		Message: "agent stream not found",
		Code:    errors.ErrorCodeAGENT03,
	},
)

var ErrStreamingUnavailable = errors.NewHTTPError(
	http.StatusInternalServerError,
	"Agent event streaming unavailable",
	errors.ErrorDetail{
		Message: "agent event streaming is unavailable",
		Code:    errors.ErrorCodeAGENT04,
	},
)

var ErrEmptyMessage = errors.ErrorDetail{
	Message: "message is required",
	Code:    errors.ErrorCodeAGENT01,
	Field:   "message",
}

var ErrReasonRequiredForRejection = errors.ErrorDetail{
	Message: "reason is required when rejecting an approval",
	Code:    errors.ErrorCodeAPPROVAL03,
	Field:   "reason",
}
