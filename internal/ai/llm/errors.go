package llm

import (
	"fmt"
	"time"

	"nautilus/internal/errors"
)

var (
	ErrCompletionTimeout = errors.New("completion request timed out")
	ErrTokenDelayTimeout = errors.New("timed out waiting for token")
	ErrStreamCanceled    = errors.New("stream canceled")
)

// ErrorKind categorizes API errors for appropriate handling
type ErrorKind int

const (
	ErrorKindUnknown        ErrorKind = iota
	ErrorKindRateLimit                // HTTP 429 - retry with backoff
	ErrorKindAuth                     // HTTP 401/403 - don't retry
	ErrorKindContextLength            // context_length_exceeded - truncate and retry
	ErrorKindContentPolicy            // content_policy_violation - different prompting needed
	ErrorKindServerError              // HTTP 5xx - retry
	ErrorKindInvalidRequest           // HTTP 400 - don't retry, fix request
)

// APIError wraps API errors with classification for appropriate handling
type APIError struct {
	Kind       ErrorKind
	StatusCode int
	Code       string        // original error code from API
	Message    string        // error message
	Param      string        // which parameter caused the error (if applicable)
	RetryAfter time.Duration // for rate limits
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("API error [%s] (status %d): %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// IsRetryable returns true if the error should be retried (rate limits and server errors)
func (e *APIError) IsRetryable() bool {
	return e.Kind == ErrorKindRateLimit || e.Kind == ErrorKindServerError
}

// ShouldBackoff returns true if the error requires backoff before retry (rate limits)
func (e *APIError) ShouldBackoff() bool {
	return e.Kind == ErrorKindRateLimit
}
