package noop

import (
	"context"

	"nautilus/internal/observability/llmtrace"
)

var _ llmtrace.LLMTracer = (*Tracer)(nil)
var _ llmtrace.Span = (*Span)(nil)

type Tracer struct{}

func NewTracer() *Tracer {
	return &Tracer{}
}

func (t *Tracer) Start(ctx context.Context, _ *llmtrace.Call) (context.Context, llmtrace.Span) {
	return ctx, &Span{}
}

type Span struct{}

func (s *Span) End(_ *llmtrace.Result) {}

func (s *Span) RecordError(_ error) {}
