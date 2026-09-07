package temporal

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.temporal.io/sdk/worker"

	"nautilus/internal/config"
	"nautilus/internal/log"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
)

func Worker() error {
	config.LoadDotenv()
	queues, err := temporal.TaskQueues()
	if err != nil {
		return err
	}
	ctx := log.WithContext(context.Background(), log.InferLogger("worker"))
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
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

func Smoke() error {
	config.LoadDotenv()
	queues, err := temporal.TaskQueues()
	if err != nil {
		return err
	}
	logger := log.InferLogger("temporal-smoke")
	ctx := log.WithContext(context.Background(), logger)
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
			if err := smoke.Check(ctx, c, queue); err != nil {
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
