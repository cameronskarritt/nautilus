package llm

import (
	"testing"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/observability/llmtrace"
	"nautilus/internal/testutil/require"
)

func TestTraceTokenStreamEndsSpanWithStreamResult(t *testing.T) {
	t.Parallel()

	tokens := make(chan Token)
	span := &recordingLLMSpan{}
	stream := TraceTokenStream(WithMetrics(NewTokenStream(tokens, func() {})), span)

	go func() {
		defer close(tokens)
		tokens <- &TextToken{Type: enums.TokenTypeReasoning, Text: "thinking"}
		tokens <- &TextToken{Type: enums.TokenTypeText, Text: "hello "}
		tokens <- &ToolCallToken{Type: enums.TokenTypeToolCall, Arguments: `{"city":"SF"}`}
		tokens <- &UsageToken{Type: enums.TokenTypeUsage, InputTokens: 3, OutputTokens: 5, TotalTokens: 8}
		tokens <- &StopToken{Type: enums.TokenTypeStop, Reason: "stop"}
	}()

	var forwarded []Token
	for token := range stream.Tokens() {
		forwarded = append(forwarded, token)
	}

	require.Len(t, forwarded, 5)
	require.True(t, span.Ended)
	require.Nil(t, span.Err)
	require.NotNil(t, span.Result)
	require.Equal(t, `hello {"city":"SF"}`, span.Result.Completion)
	require.Equal(t, "stop", span.Result.FinishReason)
	require.Equal(t, &llmtrace.Usage{InputTokens: 3, OutputTokens: 5, TotalTokens: 8}, span.Result.Usage)

	metricer, ok := stream.(Metricer)
	require.True(t, ok)
	require.NotNil(t, metricer.Metrics())
}

func TestTraceTokenStreamRecordsStreamErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("stream failed")
	tokens := make(chan Token)
	span := &recordingLLMSpan{}
	stream := TraceTokenStream(NewTokenStream(tokens, func() {}), span)

	go func() {
		defer close(tokens)
		tokens <- &ErrorToken{Type: enums.TokenTypeError, Err: want}
	}()

	var forwarded []Token
	for token := range stream.Tokens() {
		forwarded = append(forwarded, token)
	}

	require.Len(t, forwarded, 1)
	require.True(t, span.Ended)
	require.ErrorIs(t, span.Err, want)
	require.NotNil(t, span.Result)
}

type recordingLLMSpan struct {
	Result *llmtrace.Result
	Err    error
	Ended  bool
}

func (s *recordingLLMSpan) End(result *llmtrace.Result) {
	s.Result = result
	s.Ended = true
}

func (s *recordingLLMSpan) RecordError(err error) {
	s.Err = err
}
