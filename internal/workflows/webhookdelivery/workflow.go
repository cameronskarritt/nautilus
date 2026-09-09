package webhookdelivery

import (
	"hash/fnv"
	"strconv"
	"time"
	"uuid"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/enums"
)

const Name = "WebhookDelivery"
const sendName = "SendWebhookAttempt"
const completeName = "CompleteWebhookDelivery"
const queueName = string(enums.QueueWebhooks)
const lifetime = 24 * time.Hour

type Input struct {
	OrganizationID int    `json:"organization_id"`
	DeliveryID     string `json:"delivery_id"`
}

func (i *Input) normalize() error {
	id, err := uuid.Parse(i.DeliveryID)
	if i.OrganizationID <= 0 || err != nil {
		return temporal.NewNonRetryableApplicationError("invalid webhook delivery identifiers", "InvalidWebhookDelivery", nil) //nolint:wrapcheck // Preserve Temporal's nonretryable classification.
	}
	i.DeliveryID = id.String()
	return nil
}

type Completion struct {
	Input  Input                `json:"input"`
	Status enums.DeliveryStatus `json:"status"`
}

func Register(reg worker.Registry, a Activities) {
	reg.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: Name})
	reg.RegisterWorkflowWithOptions(TestWorkflow, workflow.RegisterOptions{Name: testName})
	reg.RegisterWorkflowWithOptions(ReplayWorkflow, workflow.RegisterOptions{Name: replayName})
	reg.RegisterActivityWithOptions(a.PrepareTest, activity.RegisterOptions{Name: prepareTestName})
	reg.RegisterActivityWithOptions(a.PrepareReplay, activity.RegisterOptions{Name: prepareReplayName})
	reg.RegisterActivityWithOptions(a.Send, activity.RegisterOptions{Name: sendName})
	reg.RegisterActivityWithOptions(a.Complete, activity.RegisterOptions{Name: completeName})
}

func Workflow(ctx workflow.Context, input Input) (err error) {
	if err := input.normalize(); err != nil {
		return err
	}
	defer func() {
		if ctx.Err() != nil {
			cleanup, cancel := workflow.NewDisconnectedContext(ctx)
			defer cancel()
			if cleanupErr := complete(cleanup, input, enums.DeliveryStatusCanceled); cleanupErr != nil {
				err = cleanupErr
			}
		}
	}()
	deadline := workflow.Now(ctx).Add(lifetime)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: time.Minute,
		RetryPolicy:            &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	for attempt := 0; workflow.Now(ctx).Before(deadline); attempt++ {
		var status enums.DeliveryStatus
		err := workflow.ExecuteActivity(ctx, sendName, input).Get(ctx, &status)
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // Preserve workflow cancellation.
		}
		if err == nil && status.IsTerminal() {
			return nil
		}
		delay := min(backoff(input, attempt), deadline.Sub(workflow.Now(ctx)))
		if delay > 0 {
			if err := workflow.Sleep(ctx, delay); err != nil {
				return err //nolint:wrapcheck // Preserve workflow cancellation.
			}
		}
	}
	return complete(ctx, input, enums.DeliveryStatusFailed)
}

func complete(ctx workflow.Context, input Input, status enums.DeliveryStatus) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
	})
	return workflow.ExecuteActivity(ctx, completeName, Completion{Input: input, Status: status}).Get(ctx, nil) //nolint:wrapcheck // Preserve activity retry exhaustion and cancellation.
}

func backoff(input Input, attempt int) time.Duration {
	delay := 5 * time.Second * time.Duration(1<<min(attempt, 10))
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(input.DeliveryID + ":" + strconv.Itoa(attempt)))
	return min(delay+delay*time.Duration(hash.Sum32()%1000)/4000, time.Hour)
}
