package inmemory

import (
	"context"
	"encoding/json"
	"sync"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/pubsub"
)

var _ pubsub.PubSub = new(MemoryPubSub)

const defaultChannelBuffer = 100

// PubSubBroker is the shared broker that handles fan-out to all subscribers.
type PubSubBroker struct {
	subscribers map[string]map[chan []byte]struct{} // topic -> set of channels
	mu          sync.RWMutex
}

// NewPubSubBroker creates a new shared broker for pubsub instances.
func NewPubSubBroker() *PubSubBroker {
	return &PubSubBroker{
		subscribers: make(map[string]map[chan []byte]struct{}),
	}
}

func (b *PubSubBroker) register(topic string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[chan []byte]struct{})
	}
	b.subscribers[topic][ch] = struct{}{}
}

func (b *PubSubBroker) unregister(topic string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscribers[topic] != nil {
		delete(b.subscribers[topic], ch)
		if len(b.subscribers[topic]) == 0 {
			delete(b.subscribers, topic)
		}
	}
}

func (b *PubSubBroker) publish(ctx context.Context, topic string, data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	logger := log.FromContext(ctx)

	channels := b.subscribers[topic]
	for ch := range channels {
		select {
		case ch <- data:
		default:
			logger.Warn("pubsub channel full, dropping message", "topic", topic)
		}
	}
}

// MemoryPubSub is an in-memory pubsub implementation.
type MemoryPubSub struct {
	broker *PubSubBroker
}

// NewMemoryPubSub creates a new pubsub instance connected to the given broker.
func NewMemoryPubSub(broker *PubSubBroker) *MemoryPubSub {
	return &MemoryPubSub{broker: broker}
}

// Publish sends a message to all subscribers of the topic.
func (p *MemoryPubSub) Publish(ctx context.Context, topic string, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "unable to marshal message")
	}

	p.broker.publish(ctx, topic, data)
	return nil
}

// Subscribe creates an independently owned topic subscription.
func (p *MemoryPubSub) Subscribe(_ context.Context, topic string) (pubsub.Subscription, error) {
	sub := &memorySubscription{
		broker:   p.broker,
		topic:    topic,
		messages: make(chan []byte, defaultChannelBuffer),
	}
	p.broker.register(topic, sub.messages)
	return sub, nil
}

type memorySubscription struct {
	broker    *PubSubBroker
	topic     string
	messages  chan []byte
	closeOnce sync.Once
}

func (s *memorySubscription) Messages() <-chan []byte {
	return s.messages
}

func (s *memorySubscription) Close() error {
	s.closeOnce.Do(func() {
		s.broker.unregister(s.topic, s.messages)
		close(s.messages)
	})
	return nil
}
