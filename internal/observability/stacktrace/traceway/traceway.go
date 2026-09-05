package traceway

import (
	"context"

	"nautilus/internal/observability/stacktrace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var _ stacktrace.StackTracer = (*StackTracer)(nil)

type StackTracer struct{}

func NewStackTracer() *StackTracer {
	return &StackTracer{}
}

func (t *StackTracer) Capture(ctx context.Context, err error, _ *stacktrace.CaptureOptions) {
	if err == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	span.AddEvent("exception", trace.WithAttributes(ExceptionAttributes(err)...))
	span.SetStatus(codes.Error, err.Error())
}

func ExceptionAttributes(err error) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("exception.type", stacktrace.ExceptionType(err)),
		attribute.String("exception.message", err.Error()),
		attribute.String("exception.stacktrace", stacktrace.ExceptionStackTrace(err)),
	}
}
