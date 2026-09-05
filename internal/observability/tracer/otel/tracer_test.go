package otel

import (
	"context"
	"testing"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/observability/tracer"
	"nautilus/internal/testutil/require"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracerRecordsSpanData(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tr := New(provider, "test")

	startedAt := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(time.Second)
	ctx, span := tr.Start(context.Background(), "db.exec", tracer.WithStartAttributes(
		tracer.StringAttr("db.system", "postgres"),
		tracer.IntAttr("db.rows", 2),
		tracer.Int64Attr("db.duration_ms", 42),
		tracer.BoolAttr("db.cached", true),
		tracer.Float64Attr("db.cost", 1.5),
	))

	require.NotNil(t, ctx)
	span.SetAttributes(tracer.StringAttr("db.query", "select 1"))
	span.AddEvent("retry", tracer.WithTimestamp(startedAt), tracer.WithAttributes(tracer.StringAttr("reason", "timeout")))
	span.SetStatus(tracer.StatusOk, "ok")
	span.End(tracer.WithEndTimestamp(endedAt))

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "db.exec", ended[0].Name())
	require.Equal(t, codes.Ok, ended[0].Status().Code)
	require.Equal(t, endedAt, ended[0].EndTime())

	attrs := spanAttributes(ended[0].Attributes())
	require.Equal(t, "postgres", attrs["db.system"])
	require.Equal(t, int64(2), attrs["db.rows"])
	require.Equal(t, int64(42), attrs["db.duration_ms"])
	require.Equal(t, true, attrs["db.cached"])
	require.Equal(t, 1.5, attrs["db.cost"])
	require.Equal(t, "select 1", attrs["db.query"])

	events := ended[0].Events()
	require.Len(t, events, 1)
	require.Equal(t, "retry", events[0].Name)
	require.Equal(t, startedAt, events[0].Time)
	eventAttrs := spanAttributes(events[0].Attributes)
	require.Equal(t, "timeout", eventAttrs["reason"])
}

func TestSpanRecordsErrors(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tr := New(provider, "test")

	_, span := tr.Start(context.Background(), "handler")
	want := errors.New("boom")
	span.RecordError(want)
	span.SetStatus(tracer.StatusError, want.Error())
	span.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, codes.Error, ended[0].Status().Code)
	require.Equal(t, want.Error(), ended[0].Status().Description)
	require.Len(t, ended[0].Events(), 1)
	require.Equal(t, "exception", ended[0].Events()[0].Name)
	eventAttrs := spanAttributes(ended[0].Events()[0].Attributes)
	require.Equal(t, want.Error(), eventAttrs["exception.message"])
	require.Contains(t, eventAttrs["exception.stacktrace"].(string), "tracer_test.go")
}

func spanAttributes(attrs []attribute.KeyValue) map[string]any {
	values := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.AsInterface()
	}
	return values
}
