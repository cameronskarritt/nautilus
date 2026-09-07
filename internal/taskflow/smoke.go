package taskflow

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/errors"
)

const smokeResult = "Temporal activity completed"

func Smoke(ctx workflow.Context) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var result string
	err := workflow.ExecuteActivity(ctx, SmokeActivity).Get(ctx, &result)
	return result, errors.Wrap(err, "run Temporal smoke activity")
}

func SmokeActivity(context.Context) (string, error) {
	return smokeResult, nil
}

func RunSmoke(ctx context.Context, c client.Client, queue string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       "temporal-smoke-" + uuid.NewString(),
		TaskQueue:                queue,
		WorkflowExecutionTimeout: time.Minute,
	}, Smoke)
	if err != nil {
		return errors.Wrapf(err, "start Temporal smoke workflow on queue %q", queue)
	}
	var result string
	if err := run.Get(ctx, &result); err != nil {
		return errors.Wrapf(err, "await Temporal smoke workflow on queue %q", queue)
	}
	if result != smokeResult {
		return errors.New("unexpected Temporal smoke result")
	}
	return nil
}
