package upload_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
	"nautilus/internal/workflows/webhookdelivery"
)

func TestPublishWebhookAtomicityAndRetry(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	orgID := testutil.CreateTestOrg(t, db, "publish", "Publish")
	doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "private.txt", ContentType: "text/plain", Size: 1})
	require.NoError(t, err)
	createUploadHook(t, db, orgID)
	input := upload.Input{OrganizationID: orgID, DocumentID: doc.ExternalID}
	err = (upload.Activities{DB: failingPublicationDB{db}}).Finalize(t.Context(), input)
	require.Error(t, err)
	current, err := documents.GetByExternalID(t.Context(), db, orgID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, enums.DocumentStatusUploading, current.Status)
	event, err := webhookevents.GetByKey(t.Context(), db, orgID, "document.available:"+doc.ExternalID)
	require.NoError(t, err)
	require.Nil(t, event)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM webhook_deliveries WHERE organization_id = $1`, orgID).Scan(&count))
	require.Zero(t, count)

	a := upload.Activities{DB: db}
	require.NoError(t, a.Finalize(t.Context(), input))
	event, err = webhookevents.GetByKey(t.Context(), db, orgID, "document.available:"+doc.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, event)
	first, err := a.WebhookDeliveries(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, first, 1)

	// Simulate a committed finalization whose activity result was lost. New
	// subscribers must not receive a historical occurrence when it retries.
	createUploadHook(t, db, orgID)
	require.NoError(t, a.Finalize(t.Context(), input))
	again, err := webhookevents.GetByKey(t.Context(), db, orgID, event.IdempotencyKey)
	require.NoError(t, err)
	require.Equal(t, event, again)
	ids, err := a.WebhookDeliveries(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, first, ids)
	current, err = documents.GetByExternalID(t.Context(), db, orgID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, enums.DocumentStatusUploaded, current.Status)
	require.True(t, current.UpdatedAt.Equal(event.OccurredAt))
}

func TestRecoverPreWebhookFinalization(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	orgID := testutil.CreateTestOrg(t, db, "recovery", "Recovery")
	doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "private.txt", ContentType: "text/plain", Size: 1})
	require.NoError(t, err)
	doc, err = documents.MarkUploaded(t.Context(), db, orgID, doc.ExternalID)
	require.NoError(t, err)
	createUploadHook(t, db, orgID)
	a := upload.Activities{DB: db}
	ids, err := a.WebhookDeliveries(t.Context(), upload.Input{OrganizationID: orgID, DocumentID: doc.ExternalID})
	require.NoError(t, err)
	require.Len(t, ids, 1)
	event, err := webhookevents.GetByKey(t.Context(), db, orgID, "document.available:"+doc.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.True(t, doc.UpdatedAt.Equal(event.OccurredAt))
}

func TestUploadWebhookChildScheduling(t *testing.T) {
	t.Parallel()
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "starts before OCR", true: "old history"}[old], func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			upload.Register(env, upload.Activities{})
			webhookdelivery.Register(env, webhookdelivery.Activities{})
			input := upload.Input{OrganizationID: 1, DocumentID: uuid.NewV4().String()}
			deliveryID := uuid.NewV4().String()
			started := false
			env.OnActivity("FinalizeUpload", mock.Anything, input).Return(nil).Once()
			if old {
				env.OnGetVersion("upload-webhooks", workflow.DefaultVersion, workflow.Version(1)).Return(workflow.DefaultVersion).Once()
			} else {
				env.OnActivity("UploadWebhookDeliveries", mock.Anything, input).Return([]string{deliveryID}, nil).Once()
				env.OnWorkflow(webhookdelivery.Name, mock.Anything, webhookdelivery.Input{OrganizationID: input.OrganizationID, DeliveryID: deliveryID}).Return(nil).Once()
				env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, _ converter.EncodedValues) {
					started = true
					require.Equal(t, "webhook-delivery-"+strconv.Itoa(input.OrganizationID)+"-"+deliveryID, info.WorkflowExecution.ID)
				})
			}
			env.OnActivity("OCRUpload", mock.Anything, input).Return(func(context.Context, upload.Input) error {
				require.Equal(t, !old, started)
				return nil
			}).Once()
			env.OnActivity("IndexUpload", mock.Anything, input).Return(nil).Once()
			env.ExecuteWorkflow(upload.Name, input)
			require.NoError(t, env.GetWorkflowError())
			env.AssertExpectations(t)
		})
	}
}

func createUploadHook(t *testing.T, db database.Database, orgID int) {
	t.Helper()
	_, err := webhooks.Create(t.Context(), db, orgID, &webhooks.CreateOptions{
		ExternalID: uuid.NewV4().String(), Name: "Uploads", URL: "https://example.com/webhook",
		EventTypes: []enums.WebhookEventType{enums.WebhookEventTypeDocumentAvailable}, SigningSecret: []byte("encrypted-secret"),
	})
	require.NoError(t, err)
}

type failingPublicationDB struct{ database.Database }

func (db failingPublicationDB) Begin(ctx context.Context) (database.Transaction, error) {
	tx, err := db.Database.Begin(ctx)
	return failingPublicationTx{tx}, err
}

type failingPublicationTx struct{ database.Transaction }

func (tx failingPublicationTx) Begin(ctx context.Context) (database.Transaction, error) {
	nested, err := tx.Transaction.Begin(ctx)
	return failingPublicationTx{nested}, err
}

func (tx failingPublicationTx) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	if strings.Contains(query, "INSERT INTO webhook_deliveries") {
		return nil, errors.New("injected delivery insert failure")
	}
	return tx.Transaction.Exec(ctx, query, args...)
}

func TestUploadCancellationCompletesWebhookHandoff(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"FinalizeUpload", "UploadWebhookDeliveries"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, "cancel-upload", "Cancel Upload")
			doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "file.txt", ContentType: "text/plain", Size: 1})
			require.NoError(t, err)
			createUploadHook(t, db, orgID)
			input := upload.Input{OrganizationID: orgID, DocumentID: doc.ExternalID}
			a := upload.Activities{DB: db}
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			upload.Register(env, a)
			env.RegisterWorkflowWithOptions(func(ctx workflow.Context, _ webhookdelivery.Input) error {
				return workflow.Sleep(ctx, time.Hour) //nolint:wrapcheck // Preserve the test child's cancellation.
			}, workflow.RegisterOptions{Name: webhookdelivery.Name})
			env.OnActivity("FinalizeUpload", mock.Anything, input).Return(func(ctx context.Context, input upload.Input) error {
				err := a.Finalize(ctx, input)
				if stage == "FinalizeUpload" {
					env.CancelWorkflow()
				}
				return err
			}).Once()
			env.OnActivity("UploadWebhookDeliveries", mock.Anything, input).Return(func(ctx context.Context, input upload.Input) ([]string, error) {
				ids, err := a.WebhookDeliveries(ctx, input)
				if stage == "UploadWebhookDeliveries" {
					env.CancelWorkflow()
				}
				return ids, err
			}).Once()
			var started string
			completed := false
			env.SetOnChildWorkflowCompletedListener(func(_ *workflow.Info, _ converter.EncodedValue, err error) {
				require.NoError(t, err)
				completed = true
			})
			canceled := false
			env.SetOnChildWorkflowCanceledListener(func(*workflow.Info) { canceled = true })
			env.SetOnChildWorkflowStartedListener(func(_ *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
				var child webhookdelivery.Input
				require.NoError(t, args.Get(&child))
				require.Equal(t, orgID, child.OrganizationID)
				started = child.DeliveryID
			})
			// Keep the test environment running after Upload returns so cancellation
			// commands from deferred cleanup cannot escape observation.
			env.ExecuteWorkflow(func(ctx workflow.Context, input upload.Input) error {
				err := upload.Workflow(ctx, input)
				observe, _ := workflow.NewDisconnectedContext(ctx)
				if sleepErr := workflow.Sleep(observe, 2*time.Hour); sleepErr != nil {
					return sleepErr //nolint:wrapcheck // Preserve test workflow failure.
				}
				return err
			}, input)
			require.True(t, temporal.IsCanceledError(env.GetWorkflowError()), env.GetWorkflowError())
			require.NotEmpty(t, started)
			require.False(t, canceled, "handoff must not cancel the independent delivery child")
			require.True(t, completed, "delivery child should finish after Upload returns")
			deliveries, err := a.WebhookDeliveries(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, []string{started}, deliveries)
			env.AssertExpectations(t)
		})
	}
}

func TestUploadHandoffAfterOrganizationDeletion(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	orgID := testutil.CreateTestOrg(t, db, "deleted-upload", "Deleted Upload")
	doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "file.txt", ContentType: "text/plain", Size: 1})
	require.NoError(t, err)
	createUploadHook(t, db, orgID)
	input := upload.Input{OrganizationID: orgID, DocumentID: doc.ExternalID}
	a := upload.Activities{DB: db}
	require.NoError(t, a.Finalize(t.Context(), input))
	ids, err := a.WebhookDeliveries(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.NoError(t, organizations.Delete(t.Context(), db, orgID))
	after, err := a.WebhookDeliveries(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, ids, after)
	// The internal handoff path must not broaden to another organization's IDs.
	otherID := testutil.CreateTestOrg(t, db, "other-upload", "Other Upload")
	_, err = a.WebhookDeliveries(t.Context(), upload.Input{OrganizationID: otherID, DocumentID: doc.ExternalID})
	require.Error(t, err)
}
