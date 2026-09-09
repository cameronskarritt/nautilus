package upload

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/temporal/failure"
	"nautilus/internal/testutil/require"
)

func TestHeartbeatOutsideActivity(t *testing.T) {
	t.Parallel()
	ctx, stop := heartbeat(t.Context())
	stop()
	require.Equal(t, t.Context(), ctx)
	require.NoError(t, ctx.Err())
}

func TestHeartbeat(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cleanup", "worker stop", "caller cancellation"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestActivityEnvironment()
			env.SetFailureConverter(failure.NewConverter())
			env.SetTestTimeout(5 * time.Second)
			workerStop := make(chan struct{})
			env.SetWorkerStopChannel(workerStop)
			beat := make(chan bool, 1)
			env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) { beat <- details.HasValues() })
			run := func(ctx context.Context) error {
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				ctx, stop := heartbeat(ctx)
				defer stop()
				select {
				case hasDetails := <-beat:
					require.False(t, hasDetails)
				case <-time.After(2 * time.Second):
					t.Fatal("initial heartbeat was not sent")
				}
				switch mode {
				case "cleanup":
					stop()
				case "worker stop":
					close(workerStop)
				case "caller cancellation":
					cancel()
				}
				select {
				case <-ctx.Done():
					require.ErrorIs(t, ctx.Err(), context.Canceled)
				case <-time.After(2 * time.Second):
					t.Fatal("OCR context was not canceled")
				}
				// stop waits for the heartbeat goroutine and is safe after cancellation.
				stop()
				return nil
			}
			env.RegisterActivity(run)
			_, err := env.ExecuteActivity(run)
			require.NoError(t, err)
		})
	}
}

func TestHeartbeatCanceledContext(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.SetFailureConverter(failure.NewConverter())
	env.SetTestTimeout(5 * time.Second)
	var beats atomic.Int32
	env.SetOnActivityHeartbeatListener(func(*activity.Info, converter.EncodedValues) { beats.Add(1) })
	run := func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		ctx, stop := heartbeat(ctx)
		stop()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		return nil
	}
	env.RegisterActivity(run)
	_, err := env.ExecuteActivity(run)
	require.NoError(t, err)
	require.Zero(t, beats.Load())
}
