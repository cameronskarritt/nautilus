package upload

import (
	"time"
	"uuid"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/kms"
	"nautilus/internal/objectstore"
	"nautilus/internal/ocr"
	"nautilus/internal/search"
	"nautilus/internal/temporal/failure"
	"nautilus/internal/workflows/webhookdelivery"
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
		return failure.New("invalid upload identifiers", "InvalidUpload", true)
	}
	i.DocumentID = id.String()
	return nil
}

type Activities struct {
	DB      database.Database
	Store   objectstore.Store
	Keys    kms.KeyManager
	OCR     ocr.OCR
	Indexer search.Indexer
}

func Register(reg worker.Registry, a Activities) {
	reg.RegisterActivityWithOptions(a.WebhookDeliveries, activity.RegisterOptions{Name: "UploadWebhookDeliveries"})
	reg.RegisterActivityWithOptions(a.Index, activity.RegisterOptions{Name: "IndexUpload"})
	reg.RegisterActivityWithOptions(a.Extract, activity.RegisterOptions{Name: "OCRUpload"})
	reg.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: Name})
	reg.RegisterActivityWithOptions(a.Finalize, activity.RegisterOptions{Name: activityName})
}

func Workflow(ctx workflow.Context, input Input) error {
	if err := input.normalize(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
	})
	if workflow.GetVersion(ctx, "upload-source-pages", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Hour,
			HeartbeatTimeout:    30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
		})
	}
	dispatchWebhooks := workflow.GetVersion(ctx, "upload-webhooks", workflow.DefaultVersion, 1) != workflow.DefaultVersion
	publication := ctx
	if dispatchWebhooks {
		// Once publication begins, cancellation must not strand committed deliveries
		// before their independent workflows have started.
		publication, _ = workflow.NewDisconnectedContext(ctx)
	}
	if err := workflow.ExecuteActivity(publication, activityName, input).Get(publication, nil); err != nil {
		return errors.Wrap(err, "finalize upload")
	}
	if dispatchWebhooks {
		dispatch := workflow.WithActivityOptions(publication, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
		})
		var deliveries []string
		if err := workflow.ExecuteActivity(dispatch, "UploadWebhookDeliveries", input).Get(publication, &deliveries); err != nil {
			return errors.Wrap(err, "read upload webhook deliveries")
		}
		for _, id := range deliveries {
			if err := webhookdelivery.StartChild(publication, webhookdelivery.Input{OrganizationID: input.OrganizationID, DeliveryID: id}); err != nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return errors.Wrap(ctx.Err(), "upload canceled after webhook handoff")
		}
	}
	if workflow.GetVersion(ctx, "upload-ocr", workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return nil
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
	})
	if err := workflow.ExecuteActivity(ctx, "OCRUpload", input).Get(ctx, nil); err != nil {
		return errors.Wrap(err, "extract upload text")
	}
	if workflow.GetVersion(ctx, "upload-index", workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return nil
	}
	// Local embedding batches can each take a minute. Keep their full document
	// replacement in one activity, with enough time for all eight batches.
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
	})
	return errors.Wrap(workflow.ExecuteActivity(ctx, "IndexUpload", input).Get(ctx, nil), "index upload")
}
