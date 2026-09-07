package orchestration

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"nautilus/internal/config"
	"nautilus/internal/log"
	"nautilus/internal/taskflow"
)

func Worker() error {
	config.LoadDotenv()
	queues, err := taskflow.TaskQueues()
	if err != nil {
		return err
	}
	ctx := log.WithContext(context.Background(), log.InferLogger("worker"))
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := taskflow.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return taskflow.RunWorkers(ctx, c, queues)
}

func Smoke() error {
	config.LoadDotenv()
	queues, err := taskflow.TaskQueues()
	if err != nil {
		return err
	}
	logger := log.InferLogger("temporal-smoke")
	ctx := log.WithContext(context.Background(), logger)
	c, err := taskflow.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	group, ctx := errgroup.WithContext(ctx)
	for _, queue := range queues {
		group.Go(func() error {
			if err := taskflow.RunSmoke(ctx, c, queue); err != nil {
				return err
			}
			logger.Info("Temporal smoke workflow and activity completed", "queue", queue)
			return nil
		})
	}
	return group.Wait() //nolint:wrapcheck // RunSmoke already contextualizes SDK errors.
}
