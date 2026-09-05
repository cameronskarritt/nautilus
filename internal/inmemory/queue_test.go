package inmemory

import (
	"context"
	"testing"
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestMemoryQueuePublishAndConsume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data any
		want string
	}{
		{name: "string", data: "message", want: `"message"`},
		{name: "object", data: map[string]string{"key": "value"}, want: `{"key":"value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broker := NewMemoryBroker()
			ctx, cancel := context.WithCancel(t.Context())
			consumed := make(chan []byte, 1)
			done := make(chan error, 1)
			go func() {
				done <- broker.Consume(ctx, enums.Queue("test"), func(_ context.Context, data []byte) error {
					consumed <- data
					cancel()
					return nil
				})
			}()

			messageID, err := broker.Publish(ctx, enums.Queue("test"), tt.data)
			require.NoError(t, err)
			require.NotEqual(t, "", messageID)
			require.JSONEq(t, tt.want, string(waitMemory(t, consumed)))
			require.NoError(t, waitMemory(t, done))
		})
	}
}

func TestMemoryQueueRetriesFailedMessages(t *testing.T) {
	t.Parallel()

	broker := NewMemoryBroker()
	ctx, cancel := context.WithCancel(t.Context())
	attempts := make(chan []byte, 2)
	done := make(chan error, 1)
	go func() {
		attempt := 0
		done <- broker.Consume(ctx, enums.Queue("test"), func(_ context.Context, data []byte) error {
			attempt++
			attempts <- data
			if attempt == 1 {
				return errors.New("try again")
			}
			cancel()
			return nil
		})
	}()

	_, err := broker.Publish(ctx, enums.Queue("test"), "retry")
	require.NoError(t, err)
	require.JSONEq(t, `"retry"`, string(waitMemory(t, attempts)))
	require.JSONEq(t, `"retry"`, string(waitMemory(t, attempts)))
	require.NoError(t, waitMemory(t, done))
}

func TestMemoryQueuePublishRejectsUnmarshalableData(t *testing.T) {
	t.Parallel()

	messageID, err := NewMemoryBroker().Publish(t.Context(), "test", make(chan int))
	require.ErrorContains(t, err, "unable to marshal data")
	require.Equal(t, "", messageID)
}

func waitMemory[T any](t *testing.T, ch <-chan T) T {
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
