package noop

import (
	"context"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/observability/llmtrace"
	"nautilus/internal/testutil/require"
)

func TestTracerStartReturnsContextAndSpan(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	tr := NewTracer()

	gotCtx, span := tr.Start(ctx, &llmtrace.Call{
		Operation: "chat",
		Provider:  "openai",
		Model:     "gpt-5-mini",
		Prompt:    "hello",
		Streaming: true,
	})

	require.Equal(t, ctx, gotCtx)
	require.NotNil(t, span)
	span.RecordError(errors.New("ignored"))
	span.End(&llmtrace.Result{
		Completion:   "world",
		Usage:        &llmtrace.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
		FinishReason: "stop",
	})
}
