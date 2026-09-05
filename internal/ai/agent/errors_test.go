package agent

import (
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestSerializeError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Err      error
		Expected string
	}{
		{
			Name:     "plain error uses Error()",
			Err:      errors.New("connection refused"),
			Expected: "connection refused",
		},
		{
			Name:     "agent error uses external message",
			Err:      NewAgentError("something went wrong", errors.New("connection refused: 10.0.0.1:5432")),
			Expected: "something went wrong",
		},
		{
			Name:     "wrapped agent error still extracts message",
			Err:      errors.Wrap(NewAgentError("bad request", errors.New("invalid JSON at offset 42")), "handler failed"),
			Expected: "bad request",
		},
		{
			Name:     "LLMRequestError hides provider details",
			Err:      LLMRequestError(errors.New("anthropic: 529 overloaded")),
			Expected: "LLM request failed",
		},
		{
			Name:     "ToolExecutionError hides tool internals",
			Err:      ToolExecutionError(errors.New("bash: permission denied /etc/shadow")),
			Expected: "Tool execution failed",
		},
		{
			Name:     "InternalError hides panic details",
			Err:      InternalError(errors.New("panic: runtime error: index out of range")),
			Expected: "Internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			payload := serializeError(tt.Err)
			require.Equal(t, tt.Expected, payload.Error)
		})
	}
}

func TestAgentError(t *testing.T) {
	t.Parallel()

	underlying := errors.New("tcp dial timeout")
	agentErr := NewAgentError("service unavailable", underlying)

	t.Run("Error returns underlying message", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "tcp dial timeout", agentErr.Error())
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, agentErr, underlying)
	})
}
