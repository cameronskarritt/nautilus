package main

import (
	"context"

	"go.temporal.io/sdk/worker"

	"nautilus/internal/errors"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
)

var registrations = map[string]func(worker.Registry){
	smoke.Queue: smoke.Register,
}

func runWorker(ctx context.Context, queue string) error {
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
	return temporal.RunWorkers(ctx, map[string]worker.Worker{queue: w})
}
