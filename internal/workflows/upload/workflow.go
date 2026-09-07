package upload

import (
	"context"
	"time"
	"uuid"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database"
	"nautilus/internal/database/documents"
	"nautilus/internal/log"
)

// Keep registered names stable across package and function renames.
const Name = "Upload"

const activityName = "FinalizeUpload"

type Input struct {
	OrganizationID int    `json:"organization_id"`
	DocumentID     string `json:"document_id"`
}

func (i *Input) normalize() error {
	id, err := uuid.Parse(i.DocumentID)
	if i.OrganizationID <= 0 || err != nil {
		return temporal.NewNonRetryableApplicationError("invalid upload identifiers", "InvalidUpload", nil) //nolint:wrapcheck // Temporal must serialize the original nonretryable error.
	}
	i.DocumentID = id.String()
	return nil
}

func Register(reg worker.Registry, db database.Database) {
	reg.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: Name})
	reg.RegisterActivityWithOptions(func(ctx context.Context, input Input) error {
		if err := input.normalize(); err != nil {
			return err
		}
		doc, err := documents.MarkUploaded(ctx, db, input.OrganizationID, input.DocumentID)
		if err != nil {
			log.FromContext(ctx).Error("unable to finalize document upload", "error", err)
			// Database errors can contain document data; only a safe error enters history.
			return temporal.NewApplicationError("unable to finalize document upload", "UploadDatabaseError")
		}
		if doc == nil {
			return temporal.NewNonRetryableApplicationError("document upload unavailable", "UploadUnavailable", nil)
		}
		return nil
	}, activity.RegisterOptions{Name: activityName})
}

func Workflow(ctx workflow.Context, input Input) error {
	if err := input.normalize(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
	})
	return workflow.ExecuteActivity(ctx, activityName, input).Get(ctx, nil) //nolint:wrapcheck // Preserve Temporal's activity failure type and retry semantics.
}
