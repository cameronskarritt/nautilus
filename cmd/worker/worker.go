package main

import (
	"context"

	"go.temporal.io/sdk/worker"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
)

var registrations = map[enums.Queue]func(worker.Registry){
	enums.QueueSmoke: smoke.Register,
}

func runWorker(ctx context.Context, queue enums.Queue) error {
	register, ok := registrations[queue]
	if !ok {
		return errors.Errorf("no workflows registered for queue %q", queue)
	}
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	w := temporal.NewWorker(c, queue)
	register(w)
	return temporal.RunWorkers(ctx, map[enums.Queue]worker.Worker{queue: w})
}
