package agent

import (
	"context"
	"time"

	"nautilus/internal/ai/llm"
	"nautilus/internal/errors"
)

// Default backoff values
const (
	defaultBaseDelay = 500 * time.Millisecond
	defaultMaxDelay  = 10 * time.Second
)

// ModelRetry wraps multiple LLM clients and provides automatic fallback
// when a provider fails with a retryable error.
type ModelRetry struct {
	clients      []llm.Client
	retryUnknown bool          // also retry on ErrorKindUnknown (network errors)
	baseDelay    time.Duration // base delay for exponential backoff
	maxDelay     time.Duration // maximum delay cap
}

// ModelRetryOption configures ModelRetry behavior.
type ModelRetryOption func(*ModelRetry)

// WithRetryUnknown enables retrying on unknown errors (e.g., network failures).
func WithRetryUnknown(retry bool) ModelRetryOption {
	return func(mr *ModelRetry) {
		mr.retryUnknown = retry
	}
}

// WithBackoff configures the exponential backoff delays.
// baseDelay is the initial delay (default 500ms), maxDelay caps the delay (default 10s).
func WithBackoff(baseDelay, maxDelay time.Duration) ModelRetryOption {
	return func(mr *ModelRetry) {
		mr.baseDelay = baseDelay
		mr.maxDelay = maxDelay
	}
}

// NewModelRetry creates a new ModelRetry wrapper with the given clients.
// Clients are tried in order; if one fails with a retryable error, the next is tried.
func NewModelRetry(clients []llm.Client, opts ...ModelRetryOption) *ModelRetry {
	mr := &ModelRetry{
		clients:   clients,
		baseDelay: defaultBaseDelay,
		maxDelay:  defaultMaxDelay,
	}
	for _, opt := range opts {
		opt(mr)
	}
	return mr
}

// shouldRetry determines if the error warrants trying the next provider.
func (mr *ModelRetry) shouldRetry(err error) bool {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		// Not an APIError - retry if retryUnknown is enabled
		return mr.retryUnknown
	}

	// Use the built-in IsRetryable which covers RateLimit and ServerError
	if apiErr.IsRetryable() {
		return true
	}

	// Optionally retry on unknown errors
	if mr.retryUnknown && apiErr.Kind == llm.ErrorKindUnknown {
		return true
	}

	return false
}

// backoffDuration returns how long to wait before retrying.
// If RetryAfter is set, it takes precedence. Otherwise, exponential backoff is used.
func (mr *ModelRetry) backoffDuration(err error, attempt int) time.Duration {
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		// Respect RetryAfter header if present
		if apiErr.ShouldBackoff() && apiErr.RetryAfter > 0 {
			return apiErr.RetryAfter
		}
	}

	// Calculate exponential backoff: baseDelay * 2^attempt
	delay := mr.baseDelay << attempt // equivalent to baseDelay * 2^attempt
	if delay > mr.maxDelay {
		delay = mr.maxDelay
	}

	return delay
}

// Completion tries each client in order until one succeeds or all fail.
func (mr *ModelRetry) Completion(ctx context.Context, request *llm.Request) (*llm.Message, error) {
	if len(mr.clients) == 0 {
		return nil, errors.New("no clients configured")
	}

	var lastErr error
	for i, client := range mr.clients {
		msg, err := client.Completion(ctx, request)
		if err == nil {
			return msg, nil
		}

		lastErr = err

		// Check if we should try the next provider
		isLastClient := i == len(mr.clients)-1
		if isLastClient || !mr.shouldRetry(err) {
			return nil, err
		}

		// Apply backoff before trying next provider
		backoff := mr.backoffDuration(err, i)
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "context done")
		case <-time.After(backoff):
		}
	}

	return nil, lastErr
}

// StreamCompletion tries each client in order until one succeeds or all fail.
func (mr *ModelRetry) StreamCompletion(ctx context.Context, request *llm.Request) (llm.TokenStream, error) {
	if len(mr.clients) == 0 {
		return nil, errors.New("no clients configured")
	}

	var lastErr error
	for i, client := range mr.clients {
		stream, err := client.StreamCompletion(ctx, request)
		if err == nil {
			return stream, nil
		}

		lastErr = err

		// Check if we should try the next provider
		isLastClient := i == len(mr.clients)-1
		if isLastClient || !mr.shouldRetry(err) {
			return nil, errors.Wrap(err, "error streaming completion")
		}

		// Apply backoff before trying next provider
		backoff := mr.backoffDuration(err, i)
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "context done")
		case <-time.After(backoff):
		}
	}

	return nil, lastErr
}
