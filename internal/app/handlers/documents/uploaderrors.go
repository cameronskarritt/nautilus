package documents

import (
	"net/http"

	"nautilus/internal/errors"
)

var (
	ErrInvalidUpload = errors.NewHTTPError(http.StatusBadRequest, "Invalid document upload", errors.ErrorDetail{
		Message: "upload must contain exactly one file part named file", Code: errors.ErrorCodeDOC04, Field: "file",
	})
	ErrUnsupportedUpload = errors.NewHTTPError(http.StatusUnsupportedMediaType, "Unsupported document upload", errors.ErrorDetail{
		Message: "content type must be multipart/form-data", Code: errors.ErrorCodeDOC05,
	})
	ErrUploadTooLarge = errors.NewHTTPError(http.StatusRequestEntityTooLarge, "Document upload too large", errors.ErrorDetail{
		Message: "file must not exceed 100 MiB and request must not exceed 100 MiB plus 64 KiB", Code: errors.ErrorCodeDOC06, Field: "file",
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
)
