package openai

import (
	"net/http"
	"testing"

	"nautilus/internal/ai/llm"
	"nautilus/internal/testutil/require"
)

func TestClassifyErrorKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name       string
		StatusCode int
		ErrorCode  string
		Expected   llm.ErrorKind
	}{
		// HTTP status code based classification
		{
			Name:       "401 unauthorized",
			StatusCode: http.StatusUnauthorized,
			ErrorCode:  "invalid_api_key",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "403 forbidden",
			StatusCode: http.StatusForbidden,
			ErrorCode:  "",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "429 rate limit",
			StatusCode: http.StatusTooManyRequests,
			ErrorCode:  "rate_limit_exceeded",
			Expected:   llm.ErrorKindRateLimit,
		},
		{
			Name:       "500 internal server error",
			StatusCode: http.StatusInternalServerError,
			ErrorCode:  "server_error",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "502 bad gateway",
			StatusCode: http.StatusBadGateway,
			ErrorCode:  "",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "503 service unavailable",
			StatusCode: http.StatusServiceUnavailable,
			ErrorCode:  "",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "504 gateway timeout",
			StatusCode: http.StatusGatewayTimeout,
			ErrorCode:  "",
			Expected:   llm.ErrorKindServerError,
		},

		// 400 status with specific error codes
		{
			Name:       "400 context length exceeded",
			StatusCode: http.StatusBadRequest,
			ErrorCode:  "context_length_exceeded",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "400 content policy violation",
			StatusCode: http.StatusBadRequest,
			ErrorCode:  "content_policy_violation",
			Expected:   llm.ErrorKindContentPolicy,
		},
		{
			Name:       "400 generic invalid request",
			StatusCode: http.StatusBadRequest,
			ErrorCode:  "invalid_request_error",
			Expected:   llm.ErrorKindInvalidRequest,
		},
		{
			Name:       "400 unknown error code",
			StatusCode: http.StatusBadRequest,
			ErrorCode:  "some_unknown_code",
			Expected:   llm.ErrorKindInvalidRequest,
		},

		// Error code based classification (when status code doesn't match)
		{
			Name:       "invalid_api_key by code",
			StatusCode: 0,
			ErrorCode:  "invalid_api_key",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "authentication_error by code",
			StatusCode: 0,
			ErrorCode:  "authentication_error",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "rate_limit_exceeded by code",
			StatusCode: 0,
			ErrorCode:  "rate_limit_exceeded",
			Expected:   llm.ErrorKindRateLimit,
		},
		{
			Name:       "context_length_exceeded by code",
			StatusCode: 0,
			ErrorCode:  "context_length_exceeded",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "content_policy_violation by code",
			StatusCode: 0,
			ErrorCode:  "content_policy_violation",
			Expected:   llm.ErrorKindContentPolicy,
		},
		{
			Name:       "server_error by code",
			StatusCode: 0,
			ErrorCode:  "server_error",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "internal_error by code",
			StatusCode: 0,
			ErrorCode:  "internal_error",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "unknown error code",
			StatusCode: 0,
			ErrorCode:  "some_new_error",
			Expected:   llm.ErrorKindUnknown,
		},

		// Case insensitivity
		{
			Name:       "uppercase error code",
			StatusCode: http.StatusBadRequest,
			ErrorCode:  "CONTEXT_LENGTH_EXCEEDED",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "mixed case error code",
			StatusCode: 0,
			ErrorCode:  "Rate_Limit_Exceeded",
			Expected:   llm.ErrorKindRateLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := classifyErrorKind(tt.StatusCode, tt.ErrorCode)
			require.Equal(t, tt.Expected, result)
		})
	}
}
