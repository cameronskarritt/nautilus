package app

import (
	"nautilus/internal/observability/stacktrace"
	"nautilus/internal/observability/stacktrace/traceway"
	"nautilus/internal/observability/tracer"
	"nautilus/internal/observability/tracer/otel"
)

func newStackTracer(appTracer tracer.Tracer) stacktrace.StackTracer {
	if _, ok := appTracer.(*otel.Tracer); ok {
		return traceway.NewStackTracer()
	}
	return nil
}
