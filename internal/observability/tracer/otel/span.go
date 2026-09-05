package otel

import (
	"context"

	"nautilus/internal/observability/stacktrace/traceway"
	"nautilus/internal/observability/tracer"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Tracer struct {
	shutdown func(context.Context) error
	tracer   trace.Tracer
}

func New(provider trace.TracerProvider, name string) *Tracer {
	return &Tracer{tracer: provider.Tracer(name)}
}

//nolint:spancheck // The returned Span transfers End ownership to the caller.
func (t *Tracer) Start(ctx context.Context, name string, opts ...tracer.StartOption) (context.Context, tracer.Span) {
	config := tracer.ApplyStartOptions(opts...)
	ctx, span := t.tracer.Start(ctx, name, trace.WithAttributes(convertAttrs(config.Attributes)...))
	return ctx, &Span{span: span}
}

type Span struct {
	span trace.Span
}

func (s *Span) SetAttributes(attrs ...tracer.Attribute) {
	s.span.SetAttributes(convertAttrs(attrs)...)
}

func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}
	s.span.AddEvent("exception", trace.WithAttributes(traceway.ExceptionAttributes(err)...))
}

func (s *Span) AddEvent(name string, opts ...tracer.EventOption) {
	config := tracer.ApplyEventOptions(opts...)
	eventOpts := []trace.EventOption{
		trace.WithAttributes(convertAttrs(config.Attributes)...),
	}
	if !config.Timestamp.IsZero() {
		eventOpts = append(eventOpts, trace.WithTimestamp(config.Timestamp))
	}
	s.span.AddEvent(name, eventOpts...)
}

func (s *Span) SetStatus(status tracer.Status, description string) {
	switch status {
	case tracer.StatusOk:
		s.span.SetStatus(codes.Ok, description)
	case tracer.StatusError:
		s.span.SetStatus(codes.Error, description)
	default:
		s.span.SetStatus(codes.Unset, description)
	}
}

func (s *Span) End(opts ...tracer.EndOption) {
	config := tracer.ApplyEndOptions(opts...)
	if config.Timestamp.IsZero() {
		s.span.End()
		return
	}
	s.span.End(trace.WithTimestamp(config.Timestamp))
}
