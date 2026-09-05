package noop

import (
	"context"

	"nautilus/internal/observability/tracer"
)

var _ tracer.Tracer = (*Tracer)(nil)
var _ tracer.Span = (*Span)(nil)

type Tracer struct{}

func NewTracer() *Tracer {
	return &Tracer{}
}

func (t *Tracer) Start(ctx context.Context, _ string, _ ...tracer.StartOption) (context.Context, tracer.Span) {
	return ctx, &Span{}
}

type Span struct{}

func (s *Span) SetAttributes(_ ...tracer.Attribute) {}

func (s *Span) RecordError(_ error) {}

func (s *Span) AddEvent(_ string, _ ...tracer.EventOption) {}

func (s *Span) SetStatus(_ tracer.Status, _ string) {}

func (s *Span) End(_ ...tracer.EndOption) {}
