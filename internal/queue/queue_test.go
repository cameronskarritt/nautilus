package queue

import (
	"context"
	"testing"
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

type testConsumer struct {
	failQueue enums.Queue
	started   chan enums.Queue
	stopped   chan enums.Queue
}

func newTestConsumer() *testConsumer {
	return &testConsumer{
		started: make(chan enums.Queue, 2),
		stopped: make(chan enums.Queue, 2),
	}
}

func (c *testConsumer) Consume(
	ctx context.Context,
	queue enums.Queue,
	_ MessageHandler,
) error {
	c.started <- queue
	if queue == c.failQueue {
		return errors.New("consume failed")
	}
	<-ctx.Done()
	c.stopped <- queue
	return nil
}

func TestRunStartsEveryHandlerAndStopsOnCancellation(t *testing.T) {
	t.Parallel()

	consumer := newTestConsumer()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, consumer, map[enums.Queue]MessageHandler{
			"first":  func(context.Context, []byte) error { return nil },
			"second": func(context.Context, []byte) error { return nil },
		})
	}()

	started := map[enums.Queue]bool{
		wait(t, consumer.started): true,
		wait(t, consumer.started): true,
	}
	require.True(t, started["first"])
	require.True(t, started["second"])

	cancel()
	require.NoError(t, wait(t, done))
	require.Len(t, consumer.stopped, 2)
}

func TestRunCancelsOtherConsumersAfterFailure(t *testing.T) {
	t.Parallel()

	consumer := newTestConsumer()
	consumer.failQueue = "failed"

	err := Run(t.Context(), consumer, map[enums.Queue]MessageHandler{
		"failed":  func(context.Context, []byte) error { return nil },
		"stopped": func(context.Context, []byte) error { return nil },
	})
	require.ErrorContains(t, err, "unable to consume queue: failed")
	require.Equal(t, enums.Queue("stopped"), wait(t, consumer.stopped))
}

type panicConsumer struct{}

func (*panicConsumer) Consume(context.Context, enums.Queue, MessageHandler) error {
	panic("boom")
}

func TestRunRecoversConsumerPanic(t *testing.T) {
	t.Parallel()

	err := Run(t.Context(), new(panicConsumer), map[enums.Queue]MessageHandler{
		"test": func(context.Context, []byte) error { return nil },
	})
	require.ErrorContains(t, err, "panic consuming queue: boom")
}

func wait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatal("timed out waiting for value")
		return zero
	}
}
