package upload

import (
	"context"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database/documents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
)

const RecoveryName = "UploadRecovery"

func StartRecovery(ctx context.Context, c client.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: "upload-recovery", TaskQueue: enums.QueueUploads.String(),
	}, RecoveryName, 0)
	var started *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &started) {
		return nil
	}
	return errors.Wrap(err, "start upload recovery")
}

func RecoveryWorkflow(ctx workflow.Context, after int) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 10 * time.Minute, HeartbeatTimeout: 30 * time.Second})
	var next int
	if err := workflow.ExecuteActivity(ctx, "RecoverUploads", after).Get(ctx, &next); err != nil {
		return errors.Wrap(err, "recover document uploads")
	}
	if err := workflow.Sleep(ctx, time.Minute); err != nil {
		return errors.Wrap(err, "wait for upload recovery")
	}
	return errors.Wrap(workflow.NewContinueAsNewError(ctx, RecoveryName, next), "continue upload recovery")
}

func (a Activities) Recover(ctx context.Context, after int) (next int, err error) {
	ctx, stop := heartbeat(ctx)
	defer stop()
	defer func() {
		if err != nil {
			err = uploadFailure("unable to recover document uploads", false)
		}
	}()
	for range 10 {
		doc, err := documents.ClaimUpload(ctx, a.DB, after)
		if err != nil {
			return after, err
		}
		if doc == nil {
			return 0, nil
		}
		after = doc.ID
		if err := a.recoverUpload(ctx, activity.GetClient(ctx), doc); err != nil {
			return after, err
		}
	}
	return after, nil
}

func (a Activities) recoverUpload(ctx context.Context, c client.Client, doc *documents.Document) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	input := Input{OrganizationID: doc.OrganizationID, DocumentID: doc.ExternalID}
	// Any accepted execution, including a closed one, owns its outcome. Failure
	// to query Temporal is never evidence that an upload was abandoned.
	_, err := c.DescribeWorkflowExecution(ctx, workflowID(input), "")
	if err == nil {
		return nil
	}
	var missing *serviceerror.NotFound
	if !errors.As(err, &missing) {
		return errors.Wrap(err, "look up upload workflow")
	}
	if !doc.UploadReady {
		pages, err := documents.ListPages(ctx, a.DB, doc.OrganizationID, doc.ExternalID)
		if err != nil {
			return err
		}
		complete := len(pages) == doc.PageCount
		if doc.PageCount == 0 {
			pages = []documents.Page{{ObjectKey: doc.ObjectKey}}
		}
		for _, page := range pages {
			_, err := a.Store.Head(ctx, page.ObjectKey)
			if errors.Is(err, objectstore.ErrNotFound) {
				complete = false
				break
			}
			if err != nil {
				return errors.Wrap(err, "inspect stored upload page")
			}
		}
		if !complete {
			return documents.FailUpload(ctx, a.DB, doc)
		}
	}
	ready, err := documents.ReadyUpload(ctx, a.DB, doc)
	if err != nil || !ready {
		return err
	}
	return Start(ctx, c, input)
}
