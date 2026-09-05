package queue

import (
	"context"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
)

type MessageHandler func(context.Context, []byte) error

type Publisher interface {
	Publish(ctx context.Context, queue enums.Queue, data any) (string, error)
}

type Consumer interface {
	Consume(ctx context.Context, queue enums.Queue, handler MessageHandler) error
}

func Run(ctx context.Context, consumer Consumer, handlers map[enums.Queue]MessageHandler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan error, len(handlers))
	for queue, handler := range handlers {
		queueCtx := log.WithContext(ctx, log.FromContext(ctx).With("queue", queue))
		go func() {
			err := consume(queueCtx, consumer, queue, handler)
			if err != nil {
				err = errors.Wrapf(err, "unable to consume queue: %s", queue)
			}
			results <- err
		}()
	}

	var firstErr error
	for range handlers {
		if err := <-results; err != nil && firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	return firstErr
}

func consume(
	ctx context.Context,
	consumer Consumer,
	queue enums.Queue,
	handler MessageHandler,
) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("panic consuming queue: %v", recovered)
		}
	}()
	return consumer.Consume(ctx, queue, handler)
}
