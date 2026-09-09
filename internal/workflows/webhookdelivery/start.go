package webhookdelivery

import (
	"context"
	"strconv"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/errors"
)

func StartTest(ctx context.Context, c client.Client, input TestInput) error {
	if err := input.normalize(); err != nil {
		return err
	}
	return startDispatch(ctx, c, testName, "webhook-test-"+strconv.Itoa(input.OrganizationID)+"-"+input.DeliveryID, input)
}

func StartReplay(ctx context.Context, c client.Client, input ReplayInput) error {
	if err := input.normalize(); err != nil {
		return err
	}
	return startDispatch(ctx, c, replayName, "webhook-replay-"+strconv.Itoa(input.OrganizationID)+"-"+input.RequestID, input)
}

func startDispatch(ctx context.Context, c client.Client, name, id string, input any) error {
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: id, TaskQueue: queueName, WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, name, input)
	if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return nil
	}
	return errors.Wrap(err, "unable to start webhook dispatch")
}

func StartChild(ctx workflow.Context, input Input) error {
	if err := input.normalize(); err != nil {
		return err
	}
	ctx = workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:            "webhook-delivery-" + strconv.Itoa(input.OrganizationID) + "-" + input.DeliveryID,
		TaskQueue:             queueName,
		ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	})
	err := workflow.ExecuteChildWorkflow(ctx, Name, input).GetChildWorkflowExecution().Get(ctx, nil)
	if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return nil
	}
	return errors.Wrap(err, "start webhook delivery child")
}
