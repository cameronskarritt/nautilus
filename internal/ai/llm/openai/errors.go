package openai

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nautilus/internal/ai/llm"
)

// APIError is the raw OpenAI API error response
type APIError struct {
	Details *APIErrorDetails `json:"error"`
}

type APIErrorDetails struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Param   any    `json:"param"`
}

func (err *APIError) Error() string {
	if err.Details == nil {
		return "received malformed error"
	}
	return fmt.Sprintf("%s: %s", err.Details.Code, err.Details.Message)
}

// ClassifyError converts an OpenAI APIError to a classified llm.APIError
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
		Code:       apiErr.Details.Code,
		Message:    apiErr.Details.Message,
	}

	// Extract param if it's a string
	if apiErr.Details.Param != nil {
		if paramStr, ok := apiErr.Details.Param.(string); ok {
			classified.Param = paramStr
		}
	}

	// Parse Retry-After header for rate limits
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			classified.RetryAfter = time.Duration(seconds) * time.Second
		}
	}

	// Classify based on status code and error code
	classified.Kind = classifyErrorKind(resp.StatusCode, apiErr.Details.Code)

	return classified
}

func classifyErrorKind(statusCode int, errorCode string) llm.ErrorKind {
	// First check HTTP status codes
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return llm.ErrorKindAuth
	case http.StatusTooManyRequests:
		return llm.ErrorKindRateLimit
	case http.StatusBadRequest:
		// Check specific error codes for 400
		switch strings.ToLower(errorCode) {
		case "context_length_exceeded":
			return llm.ErrorKindContextLength
		case "content_policy_violation":
			return llm.ErrorKindContentPolicy
		default:
			return llm.ErrorKindInvalidRequest
		}
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return llm.ErrorKindServerError
	}

	// Then check OpenAI-specific error codes
	switch strings.ToLower(errorCode) {
	case "invalid_api_key", "authentication_error":
		return llm.ErrorKindAuth
	case "rate_limit_exceeded":
		return llm.ErrorKindRateLimit
	case "context_length_exceeded":
		return llm.ErrorKindContextLength
	case "content_policy_violation":
		return llm.ErrorKindContentPolicy
	case "server_error", "internal_error":
		return llm.ErrorKindServerError
	default:
		return llm.ErrorKindUnknown
	}
}
