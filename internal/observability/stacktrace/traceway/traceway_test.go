package traceway

import (
	"context"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStackTracerCapturesExceptionEvent(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")

	err := errors.WithStack(errors.New("boom"))
	NewStackTracer().Capture(ctx, err, nil)
	span.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, codes.Error, ended[0].Status().Code)

	events := ended[0].Events()
	require.Len(t, events, 1)
	require.Equal(t, "exception", events[0].Name)

	attrs := make(map[string]any, len(events[0].Attributes))
	for _, attr := range events[0].Attributes {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}
	require.Contains(t, attrs["exception.type"].(string), "fundamental")
	require.Equal(t, err.Error(), attrs["exception.message"])
	require.Contains(t, attrs["exception.stacktrace"].(string), "traceway_test.go")
}
