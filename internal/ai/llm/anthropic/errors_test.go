package anthropic

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
		ErrorType  string
		Message    string
		Expected   llm.ErrorKind
	}{
		// HTTP status code based classification
		{
			Name:       "401 unauthorized",
			StatusCode: http.StatusUnauthorized,
			ErrorType:  "authentication_error",
			Message:    "invalid api key",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "403 forbidden",
			StatusCode: http.StatusForbidden,
			ErrorType:  "permission_error",
			Message:    "access denied",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "429 rate limit",
			StatusCode: http.StatusTooManyRequests,
			ErrorType:  "rate_limit_error",
			Message:    "too many requests",
			Expected:   llm.ErrorKindRateLimit,
		},
		{
			Name:       "500 internal server error",
			StatusCode: http.StatusInternalServerError,
			ErrorType:  "api_error",
			Message:    "internal error",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "529 overloaded",
			StatusCode: 529,
			ErrorType:  "overloaded_error",
			Message:    "overloaded",
			Expected:   llm.ErrorKindServerError,
		},

		// Context length errors
		{
			Name:       "413 request too large",
			StatusCode: http.StatusRequestEntityTooLarge,
			ErrorType:  "request_too_large",
			Message:    "request too large",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "400 context limit exceeded",
			StatusCode: http.StatusBadRequest,
			ErrorType:  "invalid_request_error",
			Message:    "input length and max_tokens exceed context limit: 198157 + 21333 > 200000, decrease input length or max_tokens and try again",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "400 context length in message",
			StatusCode: http.StatusBadRequest,
			ErrorType:  "invalid_request_error",
			Message:    "prompt exceeds context length limit",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "400 total text bytes exceeded",
			StatusCode: http.StatusBadRequest,
			ErrorType:  "invalid_request_error",
			Message:    "too many total text bytes: 17929476 >> 16000000",
			Expected:   llm.ErrorKindContextLength,
		},

		// Regular invalid request (not context length)
		{
			Name:       "400 invalid request not context related",
			StatusCode: http.StatusBadRequest,
			ErrorType:  "invalid_request_error",
			Message:    "invalid model parameter",
			Expected:   llm.ErrorKindInvalidRequest,
		},

		// Error type based classification (when status code doesn't match)
		{
			Name:       "authentication error by type",
			StatusCode: 0,
			ErrorType:  "authentication_error",
			Message:    "invalid api key",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "permission error by type",
			StatusCode: 0,
			ErrorType:  "permission_error",
			Message:    "access denied",
			Expected:   llm.ErrorKindAuth,
		},
		{
			Name:       "rate limit error by type",
			StatusCode: 0,
			ErrorType:  "rate_limit_error",
			Message:    "too many requests",
			Expected:   llm.ErrorKindRateLimit,
		},
		{
			Name:       "invalid request error by type with context limit message",
			StatusCode: 0,
			ErrorType:  "invalid_request_error",
			Message:    "exceeds context limit",
			Expected:   llm.ErrorKindContextLength,
		},
		{
			Name:       "invalid request error by type",
			StatusCode: 0,
			ErrorType:  "invalid_request_error",
			Message:    "invalid parameter",
			Expected:   llm.ErrorKindInvalidRequest,
		},
		{
			Name:       "api error by type",
			StatusCode: 0,
			ErrorType:  "api_error",
			Message:    "internal error",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "overloaded error by type",
			StatusCode: 0,
			ErrorType:  "overloaded_error",
			Message:    "overloaded",
			Expected:   llm.ErrorKindServerError,
		},
		{
			Name:       "unknown error type",
			StatusCode: 0,
			ErrorType:  "some_new_error",
			Message:    "unknown error",
			Expected:   llm.ErrorKindUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := classifyErrorKind(tt.StatusCode, tt.ErrorType, tt.Message)
			require.Equal(t, tt.Expected, result)
		})
	}
}

func TestIsContextLengthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Message  string
		Expected bool
	}{
		{
			Name:     "context limit message",
			Message:  "input length and max_tokens exceed context limit: 198157 + 21333 > 200000",
			Expected: true,
		},
		{
			Name:     "context length message",
			Message:  "prompt exceeds context length limit",
			Expected: true,
		},
		{
			Name:     "total text bytes message",
			Message:  "too many total text bytes: 17929476 >> 16000000",
			Expected: true,
		},
		{
			Name:     "uppercase context limit",
			Message:  "CONTEXT LIMIT exceeded",
			Expected: true,
		},
		{
			Name:     "not context related",
			Message:  "invalid model parameter",
			Expected: false,
		},
		{
			Name:     "empty message",
			Message:  "",
			Expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := isContextLengthError(tt.Message)
			require.Equal(t, tt.Expected, result)
		})
	}
}
