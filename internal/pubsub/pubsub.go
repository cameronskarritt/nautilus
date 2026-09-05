package pubsub

import "context"

type Publisher interface {
	Publish(ctx context.Context, topic string, message any) error
}

// Subscription receives messages until the caller closes it.
type Subscription interface {
	Messages() <-chan []byte
	Close() error
}

type Subscriber interface {
	Subscribe(ctx context.Context, topic string) (Subscription, error)
}

type PubSub interface {
	Publisher
	Subscriber
}
