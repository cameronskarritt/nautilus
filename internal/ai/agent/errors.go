package agent

import (
	"nautilus/internal/errors"
)

// AgentError wraps an error with an external-facing message for event serialization.
// The full error chain is preserved for internal logging via Unwrap.
type AgentError struct {
	message string
	err     error
}

// NewAgentError creates an AgentError with the given external message and underlying error.
func NewAgentError(message string, err error) *AgentError {
	return &AgentError{message: message, err: err}
}

func (e *AgentError) Error() string { return e.err.Error() }
func (e *AgentError) Unwrap() error { return e.err }

// errorPayload is the JSON-safe representation of an error in the event log.
type errorPayload struct {
	Error string `json:"error"`
}

// Named agent error constructors. Each wraps the underlying error with a safe
// external-facing message that gets serialized into the event log payload.

func LLMRequestError(err error) *AgentError {
	return NewAgentError("LLM request failed", err)
}

func ToolExecutionError(err error) *AgentError {
	return NewAgentError("Tool execution failed", err)
}

func InternalError(err error) *AgentError {
	return NewAgentError("Internal error", err)
}

// serializeError converts an error into a JSON-safe payload for event logging.
// AgentErrors use their external-facing message; plain errors use Error().
func serializeError(err error) errorPayload {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return errorPayload{Error: agentErr.message}
	}
	return errorPayload{Error: err.Error()}
}
