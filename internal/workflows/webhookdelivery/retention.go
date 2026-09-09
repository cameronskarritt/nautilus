package webhookdelivery

import (
	"context"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database/webhooks"
	"nautilus/internal/errors"
)

const RetentionName = "WebhookRetention"
const pruneName = "PruneWebhookHistory"

func RegisterRetention(reg worker.Registry, a Activities) {
	reg.RegisterWorkflowWithOptions(RetentionWorkflow, workflow.RegisterOptions{Name: RetentionName})
	reg.RegisterActivityWithOptions(a.Prune, activity.RegisterOptions{Name: pruneName})
}

func StartRetention(ctx context.Context, c client.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       "webhook-retention",
		TaskQueue:                queueName,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, RetentionName)
	return errors.Wrap(err, "unable to start webhook retention")
}

func RetentionWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Minute, MaximumInterval: time.Hour},
	})
	if err := workflow.ExecuteActivity(ctx, pruneName, workflow.Now(ctx).Add(-webhooks.Retention)).Get(ctx, nil); err != nil {
		return err //nolint:wrapcheck // Preserve Temporal retry and cancellation behavior.
	}
	if err := workflow.Sleep(ctx, 24*time.Hour); err != nil {
		return err //nolint:wrapcheck // Preserve workflow cancellation.
	}
	return workflow.NewContinueAsNewError(ctx, RetentionName) //nolint:wrapcheck // Continue with a bounded workflow history.
}

func (a Activities) Prune(ctx context.Context, before time.Time) (int, error) {
	var total int
	for range 10 {
		count, err := webhooks.Prune(ctx, a.DB, before, 1000)
		if err != nil {
			return total, unavailable()
		}
		total += count
		if count < 1000 {
			break
		}
	}
	return total, nil
}
