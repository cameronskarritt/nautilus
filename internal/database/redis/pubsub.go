package redis

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/redis/go-redis/v9"

	"nautilus/internal/errors"
	"nautilus/internal/pubsub"
)

var _ pubsub.PubSub = new(PubSub)

const defaultChannelBuffer = 100

// subscription holds the state for a single topic subscription.
type subscription struct {
	redisPubSub *redis.PubSub
	messages    chan []byte
	cancel      context.CancelFunc
	done        chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

// PubSub is a Redis-backed pubsub implementation.
type PubSub struct {
	rdb *Redis
}

// NewPubSub creates a new Redis pubsub instance.
func NewPubSub(rdb *Redis) *PubSub {
	return &PubSub{rdb: rdb}
}

// Publish sends a message to all subscribers of the topic.
func (p *PubSub) Publish(ctx context.Context, topic string, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "unable to marshal message")
	}

	err = p.rdb.client.Publish(ctx, topic, data).Err()
	if err != nil {
		return errors.Wrap(err, "unable to publish message")
	}

	return nil
}

// Subscribe creates an independently owned topic subscription.
func (p *PubSub) Subscribe(ctx context.Context, topic string) (pubsub.Subscription, error) {
	redisPubSub := p.rdb.client.Subscribe(ctx, topic)

	_, err := redisPubSub.Receive(ctx)
	if err != nil {
		_ = redisPubSub.Close()
		return nil, errors.Wrap(err, "unable to subscribe to topic")
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := &subscription{
		redisPubSub: redisPubSub,
		messages:    make(chan []byte, defaultChannelBuffer),
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	go sub.forward(subCtx)

	return sub, nil
}

func (s *subscription) Messages() <-chan []byte {
	return s.messages
}

func (s *subscription) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		if err := s.redisPubSub.Close(); err != nil {
			s.closeErr = errors.Wrap(err, "unable to close redis subscription")
		}
		<-s.done
	})
	return s.closeErr
}

func (s *subscription) forward(ctx context.Context) {
	defer close(s.done)
	defer close(s.messages)

	messages := s.redisPubSub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			select {
			case s.messages <- []byte(msg.Payload):
			default:
			}
		}
	}
}
