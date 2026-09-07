package main

import (
	"context"

	"go.temporal.io/sdk/worker"

	"nautilus/internal/log"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
)

func runWorker(ctx context.Context, queue string) error {
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	w := temporal.NewWorker(c, queue)
	smoke.Register(w)
	return temporal.RunWorkers(ctx, map[string]worker.Worker{queue: w})
}

func runSmoke(ctx context.Context, queue string) error {
	logger := log.FromContext(ctx)
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := smoke.Check(ctx, c, queue); err != nil {
		return err
	}
	logger.Info("Temporal smoke workflow and activity completed", "queue", queue)
	return nil
}
