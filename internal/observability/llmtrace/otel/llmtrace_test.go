package otel

import (
	"context"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/observability/llmtrace"
	"nautilus/internal/observability/tracer"
	"nautilus/internal/testutil/require"
)

func TestTracerRecordsGenAIAttributes(t *testing.T) {
	t.Parallel()

	recorder := &recordingTracer{}
	llmTracer := NewTracer(recorder)

	_, span := llmTracer.Start(context.Background(), &llmtrace.Call{
		Operation: "chat",
		Provider:  "openai",
		Model:     "gpt-5-mini",
		Prompt:    `{"messages":[]}`,
		Streaming: true,
	})
	span.End(&llmtrace.Result{
		Completion:   `{"content":"hello"}`,
		FinishReason: "stop",
		Usage: &llmtrace.Usage{
			InputTokens:           10,
			OutputTokens:          20,
			TotalTokens:           30,
			CachedInputTokens:     4,
			ReasoningOutputTokens: 2,
		},
	})

	got := recorder.onlySpan(t)
	require.Equal(t, "llm.chat", got.Name)
	require.True(t, got.Ended)
	require.Equal(t, tracer.StatusOk, got.Status)
	require.Equal(t, map[string]any{
		"gen_ai.operation.name":                "chat",
		"gen_ai.system":                        "openai",
		"gen_ai.request.model":                 "gpt-5-mini",
		"gen_ai.prompt":                        `{"messages":[]}`,
		"llm.streaming":                        true,
		"gen_ai.completion":                    `{"content":"hello"}`,
		"gen_ai.response.finish_reason":        "stop",
		"gen_ai.usage.input_tokens":            10,
		"gen_ai.usage.output_tokens":           20,
		"gen_ai.usage.total_tokens":            30,
		"gen_ai.usage.input_tokens.cached":     4,
		"gen_ai.usage.output_tokens.reasoning": 2,
	}, got.Attrs)
}

func TestTracerRecordsErrors(t *testing.T) {
	t.Parallel()

	recorder := &recordingTracer{}
	llmTracer := NewTracer(recorder)
	_, span := llmTracer.Start(context.Background(), &llmtrace.Call{})

	want := errors.New("provider failed")
	span.RecordError(want)
	span.End(nil)

	got := recorder.onlySpan(t)
	require.True(t, got.Ended)
	require.ErrorIs(t, got.Err, want)
	require.Equal(t, tracer.StatusError, got.Status)
	require.Equal(t, want.Error(), got.StatusDesc)
}

type recordingTracer struct {
	Spans []*recordingSpan
}

func (t *recordingTracer) Start(ctx context.Context, name string, opts ...tracer.StartOption) (context.Context, tracer.Span) {
	config := tracer.ApplyStartOptions(opts...)
	span := &recordingSpan{
		Name:  name,
		Attrs: make(map[string]any),
	}
	span.SetAttributes(config.Attributes...)
	t.Spans = append(t.Spans, span)
	return ctx, span
}

func (t *recordingTracer) onlySpan(tb testing.TB) *recordingSpan {
	tb.Helper()
	require.Len(tb, t.Spans, 1)
	return t.Spans[0]
}

type recordingSpan struct {
	Name       string
	Attrs      map[string]any
	Err        error
	Status     tracer.Status
	StatusDesc string
	Ended      bool
}

func (s *recordingSpan) SetAttributes(attrs ...tracer.Attribute) {
	for _, attr := range attrs {
		s.Attrs[attr.Key] = attr.Value
	}
}

func (s *recordingSpan) RecordError(err error) {
	s.Err = err
}

func (s *recordingSpan) AddEvent(string, ...tracer.EventOption) {}

func (s *recordingSpan) SetStatus(status tracer.Status, description string) {
	s.Status = status
	s.StatusDesc = description
}

func (s *recordingSpan) End(...tracer.EndOption) {
	s.Ended = true
}
