package temporal_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/observability/stacktrace"
	"nautilus/internal/temporal"
	"nautilus/internal/testutil/require"
)

func TestRecover(t *testing.T) {
	t.Parallel()
	cause := errors.New("activity setup failed")
	for _, tt := range []struct {
		name  string
		panic any
		text  string
	}{
		{name: "error", panic: cause, text: cause.Error()},
		{name: "string", panic: "worker bug", text: "worker bug"},
		{name: "other value", panic: 42, text: "panic: 42"},
		{name: "nil", text: "panic called with nil argument"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			ctx := log.WithContext(t.Context(), log.New(log.NewJSONHandler(&output, slog.LevelInfo)))
			tracer := &panicTracer{}
			ctx = stacktrace.WithContext(ctx, tracer)
			err := func() (err error) {
				defer temporal.Recover(ctx, &err)
				panic(tt.panic)
			}()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.text)
			if tt.panic == cause {
				require.ErrorIs(t, err, cause)
			}
			var stack errors.StackTracer
			require.ErrorAs(t, err, &stack)
			require.NotEmpty(t, stack.StackTrace())
			require.Contains(t, output.String(), tt.text)
			require.Contains(t, output.String(), "recover_test.go")
			require.Equal(t, err, tracer.err)
		})
	}
}

func TestRecoverWithoutPanic(t *testing.T) {
	t.Parallel()
	cause := errors.New("ordinary failure")
	err := func() (err error) {
		defer temporal.Recover(t.Context(), &err)
		return cause
	}()
	require.Equal(t, cause, err)
}

type panicTracer struct {
	err error
}

func (p *panicTracer) Capture(_ context.Context, err error, _ *stacktrace.CaptureOptions) {
	p.err = err
}
