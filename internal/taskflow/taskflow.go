package taskflow

import (
	"context"
	"slices"
	"strings"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"golang.org/x/sync/errgroup"

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

func TaskQueues() ([]string, error) {
	raw := config.Get("TEMPORAL_TASK_QUEUES", config.Get("TEMPORAL_TASK_QUEUE", "nautilus"))
	var queues []string
	for name := range strings.SplitSeq(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("Temporal task queues must not contain empty names")
		}
		if !slices.Contains(queues, name) {
			queues = append(queues, name)
		}
	}
	return queues, nil
}

func RunWorkers(ctx context.Context, c client.Client, queues []string) error {
	if len(queues) == 0 {
		return errors.New("at least one Temporal task queue is required")
	}
	group, ctx := errgroup.WithContext(ctx)
	interrupt := make(chan any)
	stop := context.AfterFunc(ctx, func() { close(interrupt) })
	defer stop()
	for _, queue := range queues {
		w := NewWorker(c, queue)
		group.Go(func() error {
			return errors.Wrapf(w.Run(interrupt), "run Temporal worker for queue %q", queue)
		})
	}
	return group.Wait() //nolint:wrapcheck // Worker errors already include queue context.
}

func NewWorker(c client.Client, queue string) worker.Worker {
	w := worker.New(c, queue, worker.Options{WorkerStopTimeout: 30 * time.Second})
	w.RegisterWorkflow(Smoke)
	w.RegisterActivity(SmokeActivity)
	return w
}
