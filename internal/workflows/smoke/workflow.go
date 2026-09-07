package smoke

import (
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/errors"
)

// Keep registered names stable across package and function renames.
const Name = "Smoke"

const activityName = "SmokeActivity"

func Register(reg worker.Registry) {
	reg.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: Name})
	reg.RegisterActivityWithOptions(Activity, activity.RegisterOptions{Name: activityName})
}

func Workflow(ctx workflow.Context) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var result string
	err := workflow.ExecuteActivity(ctx, activityName).Get(ctx, &result)
	return result, errors.Wrap(err, "run Temporal smoke activity")
}
