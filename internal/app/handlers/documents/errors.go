package documents

import (
	"net/http"

	"nautilus/internal/errors"
)

var (
	ErrOrganizationRequired = errors.NewHTTPError(http.StatusForbidden, "Organization required", errors.ErrorDetail{
		Message: "select an organization to access documents", Code: errors.ErrorCodeDOC01,
	})
	ErrForbidden = errors.NewHTTPError(http.StatusForbidden, "Permission denied", errors.ErrorDetail{
		Message: "you do not have permission to read documents for this organization", Code: errors.ErrorCodeDOC02,
	})
	ErrInvalidCursor = errors.NewHTTPError(http.StatusBadRequest, "Invalid document cursor", errors.ErrorDetail{
		Message: "cursor is invalid for this document list", Code: errors.ErrorCodeDOC03, Field: "cursor",
	})
)
