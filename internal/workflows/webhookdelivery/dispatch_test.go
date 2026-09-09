package webhookdelivery

import (
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestPrepareTestFreezesOneTargetOnRetry(t *testing.T) {
	t.Parallel()
	db, original, hook, _ := fixture(t)
	ctx := t.Context()
	_, err := webhooks.Update(ctx, db, original.OrganizationID, hook.ExternalID, &webhooks.UpdateOptions{EventTypes: optional.Set([]enums.WebhookEventType{enums.WebhookEventTypeDocumentAvailable})})
	require.NoError(t, err)
	_, err = webhooks.Create(ctx, db, original.OrganizationID, &webhooks.CreateOptions{ExternalID: uuid.NewV4().String(), Name: "Other subscriber", URL: "https://example.com/other", EventTypes: []enums.WebhookEventType{enums.WebhookEventTypeWebhookTest}, SigningSecret: []byte("encrypted")})
	require.NoError(t, err)
	input := TestInput{OrganizationID: original.OrganizationID, WebhookID: hook.ExternalID, DeliveryID: uuid.NewV4().String()}
	a := Activities{DB: db}
	first, err := a.PrepareTest(ctx, input)
	require.NoError(t, err)
	require.Equal(t, input.DeliveryID, first.DeliveryID)
	event, err := webhookevents.GetByKey(ctx, db, input.OrganizationID, "webhook.test:"+input.DeliveryID)
	require.NoError(t, err)
	require.NotNil(t, event)
	for range 2 {
		again, err := a.PrepareTest(ctx, input)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
	deliveries, err := webhooks.ListForEvent(ctx, db, input.OrganizationID, event.ID)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Equal(t, hook.ExternalID, deliveries[0].WebhookExternalID)
	_, err = webhooks.Update(ctx, db, input.OrganizationID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(false)})
	require.NoError(t, err)
	again, err := a.PrepareTest(ctx, input)
	require.NoError(t, err)
	require.Equal(t, first, again)
	stored, err := webhooks.GetDelivery(ctx, db, input.OrganizationID, input.DeliveryID)
	require.NoError(t, err)
	require.Equal(t, enums.DeliveryStatusCanceled, stored.Status)
	input.DeliveryID = strings.ToUpper(uuid.NewV4().String())
	canceled, err := a.PrepareTest(ctx, input)
	require.NoError(t, err)
	require.Equal(t, strings.ToLower(input.DeliveryID), canceled.DeliveryID)
	stored, err = webhooks.GetDelivery(ctx, db, input.OrganizationID, canceled.DeliveryID)
	require.NoError(t, err)
	require.Equal(t, enums.DeliveryStatusCanceled, stored.Status)
	require.True(t, stored.CompletedAt.Set)
}

func TestPrepareTestTenantBoundary(t *testing.T) {
	t.Parallel()
	db, _, hook, _ := fixture(t)
	other := testutil.CreateTestOrg(t, db, "dispatch-other", "Other")
	input := TestInput{OrganizationID: other, WebhookID: hook.ExternalID, DeliveryID: uuid.NewV4().String()}
	delivery, err := (Activities{DB: db}).PrepareTest(t.Context(), input)
	require.NoError(t, err)
	require.Empty(t, delivery.DeliveryID)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM webhook_deliveries WHERE organization_id = $1`, other).Scan(&count))
	require.Equal(t, 0, count)
}

func TestPrepareReplayPreservesEvent(t *testing.T) {
	t.Parallel()
	db, original, hook, event := fixture(t)
	a := Activities{DB: db}
	input := ReplayInput{OrganizationID: original.OrganizationID, WebhookID: hook.ExternalID, EventID: event.ExternalID, RequestID: uuid.NewV4().String(), ReplayedID: original.DeliveryID}
	first, err := a.PrepareReplay(t.Context(), input)
	require.NoError(t, err)
	require.NotEqual(t, original.DeliveryID, first.DeliveryID)
	again, err := a.PrepareReplay(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, first, again)
	delivery, err := webhooks.GetDelivery(t.Context(), db, input.OrganizationID, first.DeliveryID)
	require.NoError(t, err)
	require.Equal(t, event.ExternalID, delivery.EventExternalID)
	require.Equal(t, optional.Set(input.ReplayedID), delivery.ReplayedExternalID)
	require.Equal(t, enums.DeliveryTriggerReplay, delivery.Trigger)
	stored, err := webhookevents.GetByExternalID(t.Context(), db, input.OrganizationID, event.ExternalID)
	require.NoError(t, err)
	require.Equal(t, event.Payload, stored.Payload)
	input.ReplayedID = uuid.NewV4().String()
	_, err = a.PrepareReplay(t.Context(), input)
	var failure *temporal.ApplicationError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.NonRetryable())
}

func TestDispatchCancellationStillStartsChild(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{})
	input := TestInput{OrganizationID: 1, WebhookID: uuid.NewV4().String(), DeliveryID: uuid.NewV4().String()}
	delivery := Input{OrganizationID: 1, DeliveryID: input.DeliveryID}
	env.OnActivity(prepareTestName, mock.Anything, input).After(2*time.Second).Return(delivery, nil).Once()
	started := false
	env.SetOnChildWorkflowStartedListener(func(*workflow.Info, workflow.Context, converter.EncodedValues) { started = true })
	env.OnWorkflow(Name, mock.Anything, delivery).Return(nil).Once()
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
	env.ExecuteWorkflow(testName, input)
	require.NoError(t, env.GetWorkflowError())
	require.True(t, started)
	env.AssertExpectations(t)
}

func TestDispatchRetriesPreparationAndStartsChild(t *testing.T) {
	t.Parallel()
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "test", true: "replay"}[replay], func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			Register(env, Activities{})
			input := TestInput{OrganizationID: 1, WebhookID: uuid.NewV4().String(), DeliveryID: uuid.NewV4().String()}
			name, prepare := testName, prepareTestName
			var arg any = input
			if replay {
				name, prepare = replayName, prepareReplayName
				arg = ReplayInput{OrganizationID: 1, WebhookID: input.WebhookID, EventID: uuid.NewV4().String(), RequestID: input.DeliveryID}
			}
			delivery := Input{OrganizationID: 1, DeliveryID: input.DeliveryID}
			env.OnActivity(prepare, mock.Anything, arg).Return(Input{}, unavailable()).Once()
			env.OnActivity(prepare, mock.Anything, arg).Return(delivery, nil).Once()
			started := false
			env.SetOnChildWorkflowStartedListener(func(*workflow.Info, workflow.Context, converter.EncodedValues) { started = true })
			env.OnWorkflow(Name, mock.Anything, delivery).Return(nil).Once()
			env.ExecuteWorkflow(name, arg)
			require.NoError(t, env.GetWorkflowError())
			require.True(t, started)
			env.AssertExpectations(t)
		})
	}
}
