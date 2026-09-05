package anthropic

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nautilus/internal/ai/llm"
)

// APIError is the raw Anthropic API error response
type APIError struct {
	Type    string           `json:"type"`
	Details *APIErrorDetails `json:"error"`
}

type APIErrorDetails struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (err *APIError) Error() string {
	if err.Details == nil {
		return "received malformed error"
	}
	return fmt.Sprintf("%s: %s", err.Details.Type, err.Details.Message)
}

// ClassifyError converts an Anthropic APIError to a classified llm.APIError
func ClassifyError(resp *http.Response, apiErr *APIError) *llm.APIError {
	if apiErr == nil || apiErr.Details == nil {
		return &llm.APIError{
			Kind:       llm.ErrorKindUnknown,
			StatusCode: resp.StatusCode,
			Message:    "received malformed error response",
		}
	}

	classified := &llm.APIError{
		StatusCode: resp.StatusCode,
		Code:       apiErr.Details.Type,
		Message:    apiErr.Details.Message,
	}

	// Parse Retry-After header for rate limits
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			classified.RetryAfter = time.Duration(seconds) * time.Second
		}
	}

	// Classify based on status code and error type
	classified.Kind = classifyErrorKind(resp.StatusCode, apiErr.Details.Type, apiErr.Details.Message)

	return classified
}

func classifyErrorKind(statusCode int, errorType, message string) llm.ErrorKind {
	// First check HTTP status codes
	switch statusCode {
	case http.StatusUnauthorized:
		return llm.ErrorKindAuth
	case http.StatusForbidden:
		return llm.ErrorKindAuth
	case http.StatusTooManyRequests:
		return llm.ErrorKindRateLimit
	case http.StatusRequestEntityTooLarge: // 413 - request_too_large
		return llm.ErrorKindContextLength
	case http.StatusBadRequest:
		// Check specific error types for 400
		if strings.ToLower(errorType) == "invalid_request_error" {
			if isContextLengthError(message) {
				return llm.ErrorKindContextLength
			}
		}
		return llm.ErrorKindInvalidRequest
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return llm.ErrorKindServerError
	}

	// Then check Anthropic-specific error types
	switch strings.ToLower(errorType) {
	case "authentication_error":
		return llm.ErrorKindAuth
	case "permission_error":
		return llm.ErrorKindAuth
	case "rate_limit_error":
		return llm.ErrorKindRateLimit
	case "invalid_request_error":
		// Also check message for context length errors
		if isContextLengthError(message) {
			return llm.ErrorKindContextLength
		}
		return llm.ErrorKindInvalidRequest
	case "api_error":
		return llm.ErrorKindServerError
	case "overloaded_error":
		return llm.ErrorKindServerError
	default:
		return llm.ErrorKindUnknown
	}
}

// isContextLengthError checks if the error message indicates a context length issue.
// Anthropic returns invalid_request_error with messages like:
// - "input length and max_tokens exceed context limit: 198157 + 21333 > 200000..."
// - "too many total text bytes: 17929476 >> 16000000"
func isContextLengthError(message string) bool {
	msg := strings.ToLower(message)
	return strings.Contains(msg, "context limit") ||
		strings.Contains(msg, "context length") ||
		strings.Contains(msg, "total text bytes")
}
