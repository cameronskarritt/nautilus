package redis

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func setupTestRedis(t *testing.T) *Redis {
	t.Helper()
	ctx := context.Background()
	connStr := testutil.RedisConnString(t)
	rdb, err := Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to redis: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func TestNewPubSub(t *testing.T) {
	rdb := setupTestRedis(t)

	ps := NewPubSub(rdb)

	require.NotNil(t, ps)
	require.Equal(t, rdb, ps.rdb)
}

func TestRedisPubSub_Publish(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		Name    string
		Topic   string
		Message any
	}{
		{
			Name:    "publishes string message",
			Topic:   "test-topic-1",
			Message: "hello world",
		},
		{
			Name:  "publishes struct message",
			Topic: "test-topic-2",
			Message: map[string]string{
				"event": "user_created",
				"id":    "123",
			},
		},
		{
			Name:    "publishes to topic with no subscribers",
			Topic:   "empty-topic",
			Message: "no one listening",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			rdb := setupTestRedis(t)
			ps := NewPubSub(rdb)

			err := ps.Publish(ctx, tt.Topic, tt.Message)
			require.NoError(t, err)
		})
	}
}

func TestRedisPubSub_Publish_MarshalError(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)
	ps := NewPubSub(rdb)

	// Channels cannot be marshaled to JSON
	invalidMessage := make(chan int)

	err := ps.Publish(ctx, "test-topic", invalidMessage)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to marshal message")
}

func TestRedisPubSub_Subscribe(t *testing.T) {
	ctx := context.Background()
	rdb := setupTestRedis(t)
	ps := NewPubSub(rdb)

	sub1, err := ps.Subscribe(ctx, "test-topic")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub1.Close()) })

	sub2, err := ps.Subscribe(ctx, "test-topic")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub2.Close()) })

	require.NotEqual(t, sub1, sub2)
	require.NotEqual(t, sub1.Messages(), sub2.Messages())
}

func TestRedisPubSub_MessageFlow(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)
	ps := NewPubSub(rdb)

	topic := "message-flow-topic"

	sub, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub.Close()) })

	message := map[string]string{"event": "test", "data": "hello"}
	err = ps.Publish(ctx, topic, message)
	require.NoError(t, err)

	select {
	case received := <-sub.Messages():
		var decoded map[string]string
		err := json.Unmarshal(received, &decoded)
		require.NoError(t, err)
		require.Equal(t, "test", decoded["event"])
		require.Equal(t, "hello", decoded["data"])
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestRedisPubSub_MultipleSubscribers(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)
	ps := NewPubSub(rdb)

	topic := "multi-sub-topic"

	sub1, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)

	sub2, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub2.Close()) })

	message := "broadcast message"
	err = ps.Publish(ctx, topic, message)
	require.NoError(t, err)

	expectedData, err := json.Marshal(message)
	require.NoError(t, err)

	select {
	case received := <-sub1.Messages():
		require.Equal(t, string(expectedData), string(received))
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message on ch1")
	}

	select {
	case received := <-sub2.Messages():
		require.Equal(t, string(expectedData), string(received))
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message on ch2")
	}

	err = sub1.Close()
	require.NoError(t, err)

	err = ps.Publish(ctx, topic, "still live")
	require.NoError(t, err)

	select {
	case _, ok := <-sub1.Messages():
		require.False(t, ok)
	case <-time.After(5 * time.Second):
		t.Fatal("closed subscription channel remained open")
	}

	select {
	case received := <-sub2.Messages():
		expected, err := json.Marshal("still live")
		require.NoError(t, err)
		require.Equal(t, expected, received)
	case <-time.After(5 * time.Second):
		t.Fatal("closing sub1 stopped sub2")
	}
}
