package inmemory

import (
	"context"
	"encoding/json"
	"sync"
	"time"
	"uuid"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/queue"
)

var _ queue.Publisher = new(MemoryQueue)
var _ queue.Consumer = new(MemoryQueue)

const memoryRetryDelay = 100 * time.Millisecond

type memoryMessage struct {
	id   string
	data []byte
}

type MemoryQueue struct {
	queues map[enums.Queue]chan memoryMessage
	mu     sync.Mutex
}

func NewMemoryBroker() *MemoryQueue {
	return &MemoryQueue{
		queues: make(map[enums.Queue]chan memoryMessage),
	}
}

func (m *MemoryQueue) queue(queue enums.Queue) chan memoryMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, exists := m.queues[queue]
	if !exists {
		ch = make(chan memoryMessage, 100)
		m.queues[queue] = ch
	}
	return ch
}

func (m *MemoryQueue) Publish(ctx context.Context, topic enums.Queue, data any) (string, error) {
	logger := log.FromContext(ctx)

	buf, err := json.Marshal(data)
	if err != nil {
		return "", errors.Wrapf(err, "unable to marshal data")
	}

	id := uuid.New().String()
	logger.Info("publishing message to queue", "id", id)

	select {
	case m.queue(topic) <- memoryMessage{id: id, data: buf}:
		return id, nil
	case <-ctx.Done():
		return "", errors.Wrap(ctx.Err(), "unable to publish message")
	}
}

func (m *MemoryQueue) Consume(ctx context.Context, topic enums.Queue, handler queue.MessageHandler) error {
	logger := log.FromContext(ctx)
	ch := m.queue(topic)

	for {
		select {
		case <-ctx.Done():
			return nil
		case message := <-ch:
			for {
				if err := handler(ctx, message.data); err == nil {
					break
				} else {
					logger.Error("error consuming from queue", "message_id", message.id, "error", err)
				}

				timer := time.NewTimer(memoryRetryDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
		}
	}
}
