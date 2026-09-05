package stacktrace

import (
	"context"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestContext(t *testing.T) {
	t.Parallel()

	tracer := &recordingStackTracer{}
	ctx := WithContext(context.Background(), tracer)

	require.Equal(t, tracer, FromContext(ctx))
	require.Nil(t, FromContext(context.Background()))
}

func TestCapture(t *testing.T) {
	t.Parallel()

	tracer := &recordingStackTracer{}
	ctx := WithContext(context.Background(), tracer)
	err := errors.New("boom")

	Capture(ctx, err, nil)
	Capture(context.Background(), err, nil)
	Capture(ctx, nil, nil)

	require.Equal(t, err, tracer.err)
	require.Equal(t, 1, tracer.count)
}

func TestNewMulti(t *testing.T) {
	t.Parallel()

	first := &recordingStackTracer{}
	second := &recordingStackTracer{}
	tracer := NewMulti(nil, first, second)
	err := errors.New("boom")

	tracer.Capture(context.Background(), err, nil)

	require.Equal(t, err, first.err)
	require.Equal(t, err, second.err)
	require.Equal(t, 1, first.count)
	require.Equal(t, 1, second.count)
}

func TestExceptionAttributes(t *testing.T) {
	t.Parallel()

	err := errors.WithStack(errors.New("boom"))

	require.Contains(t, ExceptionType(err), "fundamental")
	require.Contains(t, ExceptionStackTrace(err), "stacktrace_test.go")
}

type recordingStackTracer struct {
	err   error
	count int
}

func (t *recordingStackTracer) Capture(_ context.Context, err error, _ *CaptureOptions) {
	t.err = err
	t.count++
}
