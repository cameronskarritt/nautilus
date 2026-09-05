package errors

import (
	"net/http"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestNewHTTPError(t *testing.T) {
	t.Parallel()

	detail := ErrorDetail{
		Message: "email is invalid",
		Code:    ErrorCodeAUTH03,
		Field:   "email",
	}

	err := NewHTTPError(http.StatusBadRequest, "Unable to process request", detail)

	require.Equal(t, http.StatusBadRequest, err.Status)
	require.Equal(t, "Unable to process request", err.Message)
	require.Equal(t, "Unable to process request", err.Error())
	require.Equal(t, []error{detail}, err.Errors)
}

func TestNewHTTPErrorWithoutDetails(t *testing.T) {
	t.Parallel()

	err := NewHTTPError(http.StatusBadRequest, "Unable to process request")

	require.Equal(t, http.StatusBadRequest, err.Status)
	require.Equal(t, "Unable to process request", err.Message)
	require.Empty(t, err.Errors)
}

func TestHTTPErrorDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Err      *HTTPError
		Status   int
		Expected ErrorDetail
	}{
		{
			Name:   "not found",
			Err:    ErrNotFound,
			Status: http.StatusNotFound,
			Expected: ErrorDetail{
				Message: http.StatusText(http.StatusNotFound),
				Code:    ErrorCode("HTTP-404"),
			},
		},
		{
			Name:   "method not allowed",
			Err:    ErrMethodNotAllowed,
			Status: http.StatusMethodNotAllowed,
			Expected: ErrorDetail{
				Message: http.StatusText(http.StatusMethodNotAllowed),
				Code:    ErrorCode("HTTP-405"),
			},
		},
		{
			Name:   "too many requests",
			Err:    ErrTooManyRequests,
			Status: http.StatusTooManyRequests,
			Expected: ErrorDetail{
				Message: http.StatusText(http.StatusTooManyRequests),
				Code:    ErrorCode("HTTP-429"),
			},
		},
		{
			Name:   "internal server error",
			Err:    ErrInternalServerError,
			Status: http.StatusInternalServerError,
			Expected: ErrorDetail{
				Message: http.StatusText(http.StatusInternalServerError),
				Code:    ErrorCode("HTTP-500"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Status, tt.Err.Status)
			require.Equal(t, "Unable to handle request", tt.Err.Message)
			require.Equal(t, []error{tt.Expected}, tt.Err.Errors)
		})
	}
}

func TestErrorDetail(t *testing.T) {
	t.Parallel()

	detail := ErrorDetail{
		Message: "email is invalid",
		Code:    ErrorCodeAUTH03,
		Field:   "email",
	}

	require.Equal(t, "email is invalid", detail.Error())
	require.Equal(t, "AUTH-03", detail.Code.String())
}
