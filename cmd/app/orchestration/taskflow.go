package orchestration

import (
	"context"

	"go.temporal.io/sdk/worker"

	"nautilus/internal/config"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/taskflow"
)

func Worker() error {
	config.LoadDotenv()
	ctx := log.WithContext(context.Background(), log.InferLogger("worker"))
	c, err := taskflow.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	err = taskflow.NewWorker(c, taskflow.TaskQueue()).Run(worker.InterruptCh())
	return errors.Wrap(err, "run Temporal worker")
}

func Smoke() error {
	config.LoadDotenv()
	logger := log.InferLogger("temporal-smoke")
	ctx := log.WithContext(context.Background(), logger)
	c, err := taskflow.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := taskflow.RunSmoke(ctx, c, taskflow.TaskQueue()); err != nil {
		return err
	}
	logger.Info("Temporal smoke workflow and activity completed")
	return nil
}
