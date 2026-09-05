package outbox

import (
	"context"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/outboxevents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/queue"
)

const (
	defaultLease        = 30 * time.Second
	defaultPollInterval = time.Second
)

type Dispatcher struct {
	db           database.Database
	publisher    queue.Publisher
	lease        time.Duration
	pollInterval time.Duration
}

func NewDispatcher(db database.Database, publisher queue.Publisher) *Dispatcher {
	return &Dispatcher{
		db:           db,
		publisher:    publisher,
		lease:        defaultLease,
		pollInterval: defaultPollInterval,
	}
}

func (d *Dispatcher) Run(ctx context.Context, topic enums.Queue) {
	logger := log.FromContext(ctx)

	for {
		dispatched, err := d.dispatch(ctx, topic)
		if err != nil {
			logger.Error("unable to dispatch outbox event", "topic", topic, "error", err)
		}
		if dispatched {
			continue
		}

		timer := time.NewTimer(d.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, topic enums.Queue) (bool, error) {
	event, err := outboxevents.Claim(ctx, d.db, string(topic), d.lease)
	if err != nil {
		return false, err
	}
	if event == nil {
		return false, nil
	}

	if _, err := d.publisher.Publish(ctx, topic, event.Payload); err != nil {
		return false, errors.Wrap(err, "unable to publish outbox event")
	}

	if err := outboxevents.MarkProcessed(
		ctx,
		d.db,
		event.OrganizationID,
		event.ID,
		event.LeaseToken.Data,
	); err != nil {
		return false, err
	}
	return true, nil
}
