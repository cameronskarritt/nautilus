package upload

import (
	"context"
	"strconv"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

func Start(ctx context.Context, c client.Client, input Input) error {
	if err := input.normalize(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       "upload-" + strconv.Itoa(input.OrganizationID) + "-" + input.DocumentID,
		TaskQueue:                                enums.QueueUploads.String(),
		WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, Name, input)
	var started *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &started) {
		return nil
	}
	return errors.Wrap(err, "start upload workflow")
}
