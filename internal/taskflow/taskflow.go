package taskflow

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"nautilus/internal/config"
	"nautilus/internal/errors"
	"nautilus/internal/log"
)

func Dial(ctx context.Context) (client.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := client.DialContext(ctx, client.Options{
		HostPort:  config.Get("TEMPORAL_ADDRESS", "localhost:7233"),
		Namespace: config.Get("TEMPORAL_NAMESPACE", "nautilus"),
		Logger:    log.FromContext(ctx),
	})
	if err != nil {
		return nil, errors.Wrap(err, "connect to Temporal")
	}
	return c, nil
}

func TaskQueue() string {
	return config.Get("TEMPORAL_TASK_QUEUE", "nautilus")
}

func NewWorker(c client.Client, queue string) worker.Worker {
	w := worker.New(c, queue, worker.Options{WorkerStopTimeout: 30 * time.Second})
	w.RegisterWorkflow(Smoke)
	w.RegisterActivity(SmokeActivity)
	return w
}
