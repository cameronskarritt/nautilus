package main

import (
	"context"
	"sync"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"nautilus/internal/log"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
)

func runWorker(ctx context.Context) error {
	queues, err := temporal.TaskQueues()
	if err != nil {
		return err
	}
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	workers := make(map[string]worker.Worker, len(queues))
	for _, queue := range queues {
		w := temporal.NewWorker(c, queue)
		smoke.Register(w)
		workers[queue] = w
	}
	return temporal.RunWorkers(ctx, workers)
}

func runSmoke(ctx context.Context) error {
	queues, err := temporal.TaskQueues()
	if err != nil {
		return err
	}
	logger := log.FromContext(ctx)
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var fail sync.Once
	var runErr error
	for _, queue := range queues {
		wg.Go(func() {
			if err := check(ctx, c, queue); err != nil {
				fail.Do(func() {
					runErr = err
					cancel()
				})
				return
			}
			logger.Info("Temporal smoke workflow and activity completed", "queue", queue)
		})
	}
	wg.Wait()
	return runErr
}

func check(ctx context.Context, c client.Client, queue string) (err error) {
	ctx = log.WithContext(ctx, log.FromContext(ctx).With("queue", queue))
	defer temporal.Recover(ctx, &err)
	return smoke.Check(ctx, c, queue)
}
