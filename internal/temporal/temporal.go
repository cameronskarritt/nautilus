package temporal

import (
	"context"
	"slices"
	"strings"
	"sync"
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

func TaskQueues() ([]string, error) {
	raw := config.Get("TEMPORAL_TASK_QUEUES", config.Get("TEMPORAL_TASK_QUEUE", "uploads"))
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

func RunWorkers(ctx context.Context, workers map[string]worker.Worker) error {
	if len(workers) == 0 {
		return errors.New("at least one Temporal task queue is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	interrupt := make(chan any)
	stop := context.AfterFunc(ctx, func() { close(interrupt) })
	defer stop()
	var wg sync.WaitGroup
	var fail sync.Once
	var runErr error
	for queue, w := range workers {
		wg.Go(func() {
			if err := runWorker(ctx, queue, w, interrupt); err != nil {
				fail.Do(func() {
					runErr = err
					log.FromContext(ctx).Error("Temporal worker failed", "queue", queue, "error", err.Error())
					cancel()
				})
			}
		})
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return runErr
	case <-ctx.Done():
	}
	timer := time.NewTimer(35 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		return errors.New("Temporal workers did not stop within 35 seconds")
	}
	return runErr
}

func runWorker(ctx context.Context, queue string, w worker.Worker, interrupt <-chan any) (err error) {
	ctx = log.WithContext(ctx, log.FromContext(ctx).With("queue", queue))
	defer Recover(ctx, &err)
	return errors.Wrapf(w.Run(interrupt), "run Temporal worker for queue %q", queue)
}

func NewWorker(c client.Client, queue string) worker.Worker {
	return worker.New(c, queue, worker.Options{
		WorkerStopTimeout:   30 * time.Second,
		WorkflowPanicPolicy: worker.BlockWorkflow,
	})
}
