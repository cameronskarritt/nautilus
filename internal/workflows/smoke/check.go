package smoke

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"nautilus/internal/errors"
)

func Check(ctx context.Context, c client.Client, queue string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       "temporal-smoke-" + uuid.NewString(),
		TaskQueue:                queue,
		WorkflowExecutionTimeout: time.Minute,
	}, Name)
	if err != nil {
		return errors.Wrapf(err, "start Temporal smoke workflow on queue %q", queue)
	}
	var got string
	if err := run.Get(ctx, &got); err != nil {
		return errors.Wrapf(err, "await Temporal smoke workflow on queue %q", queue)
	}
	if got != result {
		return errors.New("unexpected Temporal smoke result")
	}
	return nil
}
