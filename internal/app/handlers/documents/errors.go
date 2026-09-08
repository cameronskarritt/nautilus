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
		Message: "you do not have permission to access documents for this organization", Code: errors.ErrorCodeDOC02,
	})
	ErrInvalidCursor = errors.NewHTTPError(http.StatusBadRequest, "Invalid document cursor", errors.ErrorDetail{
		Message: "cursor is invalid for this document list", Code: errors.ErrorCodeDOC03, Field: "cursor",
	})
	ErrInvalidUpload = errors.NewHTTPError(http.StatusBadRequest, "Invalid document upload", errors.ErrorDetail{
		Message: "upload must contain only image file parts named file", Code: errors.ErrorCodeDOC04, Field: "file",
	})
	ErrUnsupportedUpload = errors.NewHTTPError(http.StatusUnsupportedMediaType, "Unsupported document upload", errors.ErrorDetail{
		Message: "content type must be multipart/form-data", Code: errors.ErrorCodeDOC05,
	})
	ErrUploadTooLarge = errors.NewHTTPError(http.StatusRequestEntityTooLarge, "Document upload too large", errors.ErrorDetail{
		Message: "images must not exceed 100 MiB combined and request must not exceed 100 MiB plus 64 KiB", Code: errors.ErrorCodeDOC06, Field: "file",
	})
	ErrInvalidFilename = errors.NewHTTPError(http.StatusBadRequest, "Invalid document filename", errors.ErrorDetail{
		Message: "filename must contain 1 to 255 valid characters without control characters", Code: errors.ErrorCodeDOC07, Field: "file.filename",
	})
	ErrStorageUnavailable = errors.NewHTTPError(http.StatusServiceUnavailable, "Document storage unavailable", errors.ErrorDetail{
		Message: "document uploads are unavailable", Code: errors.ErrorCodeDOC08,
	})
	ErrWorkflowUnavailable = errors.NewHTTPError(http.StatusServiceUnavailable, "Document processing unavailable", errors.ErrorDetail{
		Message: "document upload processing is unavailable", Code: errors.ErrorCodeDOC09,
	})
	ErrInvalidImage = errors.NewHTTPError(http.StatusUnprocessableEntity, "Invalid scan image", errors.ErrorDetail{
		Message: "each file must be a valid JPEG or PNG image of at most 25 megapixels", Code: errors.ErrorCodeDOC10, Field: "file",
	})
	ErrTooManyPages = errors.NewHTTPError(http.StatusBadRequest, "Too many scan pages", errors.ErrorDetail{
		Message: "a document must contain at most 100 page images", Code: errors.ErrorCodeDOC11, Field: "file",
	})
)
