package inmemory

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"nautilus/internal/testutil/require"
)

func TestNewPubSubBroker(t *testing.T) {
	t.Parallel()

	broker := NewPubSubBroker()

	require.NotNil(t, broker)
	require.NotNil(t, broker.subscribers)
	require.Len(t, broker.subscribers, 0)
}

func TestNewMemoryPubSub(t *testing.T) {
	t.Parallel()

	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	require.NotNil(t, ps)
	require.Equal(t, broker, ps.broker)
}

func TestMemoryPubSub_Subscribe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	sub1, err := ps.Subscribe(ctx, "test-topic")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub1.Close()) })

	sub2, err := ps.Subscribe(ctx, "test-topic")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub2.Close()) })

	require.NotEqual(t, sub1, sub2)
	require.NotEqual(t, sub1.Messages(), sub2.Messages())

	broker.mu.RLock()
	defer broker.mu.RUnlock()
	require.Len(t, broker.subscribers["test-topic"], 2)
}

func TestMemoryPubSub_SubscriptionClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	sub, err := ps.Subscribe(ctx, "test-topic")
	require.NoError(t, err)

	require.NoError(t, sub.Close())
	require.NoError(t, sub.Close())

	broker.mu.RLock()
	topicSubs := broker.subscribers["test-topic"]
	broker.mu.RUnlock()
	require.Empty(t, topicSubs)

	_, open := <-sub.Messages()
	require.False(t, open)
}

func TestMemoryPubSub_Publish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name    string
		Topic   string
		Message any
	}{
		{
			Name:    "publishes string message",
			Topic:   "test-topic",
			Message: "hello world",
		},
		{
			Name:  "publishes struct message",
			Topic: "test-topic",
			Message: struct {
				Name  string `json:"name"`
				Value int    `json:"value"`
			}{Name: "test", Value: 42},
		},
		{
			Name:    "publishes to topic with no subscribers",
			Topic:   "empty-topic",
			Message: "no one listening",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			broker := NewPubSubBroker()
			ps := NewMemoryPubSub(broker)

			var messages <-chan []byte
			if tt.Topic != "empty-topic" {
				sub, err := ps.Subscribe(ctx, tt.Topic)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, sub.Close()) })
				messages = sub.Messages()
			}

			err := ps.Publish(ctx, tt.Topic, tt.Message)
			require.NoError(t, err)

			if messages != nil {
				select {
				case data := <-messages:
					expected, err := json.Marshal(tt.Message)
					require.NoError(t, err)
					require.Equal(t, expected, data)
				case <-time.After(100 * time.Millisecond):
					t.Fatal("timeout waiting for message")
				}
			}
		})
	}
}

func TestMemoryPubSub_FanOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	topic := "shared-topic"

	sub1, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub1.Close()) })

	sub2, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub2.Close()) })

	sub3, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub3.Close()) })

	message := map[string]string{"event": "test"}
	err = ps.Publish(ctx, topic, message)
	require.NoError(t, err)

	expected, err := json.Marshal(message)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(3)

	checkChannel := func(messages <-chan []byte, name string) {
		defer wg.Done()
		select {
		case data := <-messages:
			require.Equal(t, expected, data)
		case <-time.After(100 * time.Millisecond):
			t.Errorf("%s: timeout waiting for message", name)
		}
	}

	go checkChannel(sub1.Messages(), "subscriber1")
	go checkChannel(sub2.Messages(), "subscriber2")
	go checkChannel(sub3.Messages(), "subscriber3")

	wg.Wait()

	require.NoError(t, sub1.Close())
	require.NoError(t, ps.Publish(ctx, topic, "still live"))

	_, open := <-sub1.Messages()
	require.False(t, open)

	expected, err = json.Marshal("still live")
	require.NoError(t, err)
	wg.Add(2)
	go checkChannel(sub2.Messages(), "subscriber2 after subscriber1 closed")
	go checkChannel(sub3.Messages(), "subscriber3 after subscriber1 closed")
	wg.Wait()
}

func TestMemoryPubSub_MultipleTopics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	topic1 := "topic-1"
	topic2 := "topic-2"

	sub1, err := ps.Subscribe(ctx, topic1)
	require.NoError(t, err)

	sub2, err := ps.Subscribe(ctx, topic2)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub2.Close()) })

	// Publish to different topics
	err = ps.Publish(ctx, topic1, "message-1")
	require.NoError(t, err)

	err = ps.Publish(ctx, topic2, "message-2")
	require.NoError(t, err)

	// Verify each channel only receives its topic's message
	select {
	case data := <-sub1.Messages():
		expected, err := json.Marshal("message-1")
		require.NoError(t, err)
		require.Equal(t, expected, data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for message on topic-1")
	}

	select {
	case data := <-sub2.Messages():
		expected, err := json.Marshal("message-2")
		require.NoError(t, err)
		require.Equal(t, expected, data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for message on topic-2")
	}

	err = sub1.Close()
	require.NoError(t, err)

	// Can still receive on topic2
	err = ps.Publish(ctx, topic2, "message-3")
	require.NoError(t, err)

	select {
	case data := <-sub2.Messages():
		expected, err := json.Marshal("message-3")
		require.NoError(t, err)
		require.Equal(t, expected, data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for message on topic-2 after unsubscribe from topic-1")
	}
}

func TestMemoryPubSub_Publish_ChannelFull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	topic := "test-topic"
	sub, err := ps.Subscribe(ctx, topic)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub.Close()) })

	// Fill the channel buffer (defaultChannelBuffer = 100)
	// We need to fill it completely and then try to send one more
	for i := 0; i < defaultChannelBuffer; i++ {
		err := ps.Publish(ctx, topic, "message")
		require.NoError(t, err)
	}

	// Try to publish one more - this should trigger the default case
	// where the channel is full and the message is dropped
	err = ps.Publish(ctx, topic, "dropped-message")
	require.NoError(t, err) // Publish itself doesn't error, but message is dropped

	// Verify we can still receive messages after the channel has space
	// Drain all messages to make room
	for i := 0; i < defaultChannelBuffer; i++ {
		select {
		case <-sub.Messages():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout draining channel")
		}
	}

	// Now publish should work again
	err = ps.Publish(ctx, topic, "new-message")
	require.NoError(t, err)

	select {
	case data := <-sub.Messages():
		expected, err := json.Marshal("new-message")
		require.NoError(t, err)
		require.Equal(t, expected, data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for new message")
	}
}

func TestMemoryPubSub_Publish_MarshalError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	broker := NewPubSubBroker()
	ps := NewMemoryPubSub(broker)

	// Create a value that cannot be marshaled to JSON
	// Using a channel which cannot be marshaled
	unmarshalable := make(chan int)

	err := ps.Publish(ctx, "test-topic", unmarshalable)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to marshal message")
}
