package webhookdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
	"uuid"

	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/temporal/failure"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/webhook"
)

type testKeys struct{}

func (testKeys) OrganizationKey(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{1}, 32), nil
}
func (testKeys) UserKey(context.Context) ([]byte, error) { return bytes.Repeat([]byte{2}, 32), nil }

func fixture(t *testing.T) (database.Database, Input, *webhooks.Webhook, *webhookevents.Event) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	orgID := testutil.CreateTestOrg(t, db, "delivery", "Delivery")
	org, err := organizations.Get(t.Context(), db, orgID)
	require.NoError(t, err)
	id := uuid.NewV4().String()
	secret, err := encrypt.ForOrganization(testKeys{}, org.ExternalID).Seal(t.Context(), bytes.Repeat([]byte{3}, 32), encrypt.Binding{Purpose: "webhook-signing-secret", RecordID: id})
	require.NoError(t, err)
	hook, err := webhooks.Create(t.Context(), db, orgID, &webhooks.CreateOptions{ExternalID: id, Name: "Test", URL: "https://example.com/original", EventTypes: []enums.WebhookEventType{enums.WebhookEventTypeWebhookTest}, SigningSecret: secret})
	require.NoError(t, err)
	event, _, err := webhookevents.Record(t.Context(), db, orgID, &webhookevents.CreateOptions{Type: enums.WebhookEventTypeWebhookTest, IdempotencyKey: "test", OccurredAt: time.Now()})
	require.NoError(t, err)
	deliveries, err := webhooks.CreateDeliveries(t.Context(), db, orgID, event.ID, event.Type)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	return db, Input{OrganizationID: orgID, DeliveryID: deliveries[0].ExternalID}, hook, event
}

func executeSend(t *testing.T, a Activities, input Input) (enums.DeliveryStatus, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.SetFailureConverter(failure.NewConverter())
	env.RegisterActivity(a.Send)
	result, err := env.ExecuteActivity(a.Send, input)
	if err != nil {
		return "", errors.Wrap(err, "unable to execute test activity")
	}
	var status enums.DeliveryStatus
	require.NoError(t, result.Get(&status))
	return status, nil
}

func TestSendPersistsSuccessOnce(t *testing.T) {
	t.Parallel()
	db, input, hook, event := fixture(t)
	calls := 0
	a := Activities{DB: db, Keys: testKeys{}, send: func(_ context.Context, destination, id string, payload []byte, secrets [][]byte) webhook.Result {
		calls++
		require.Equal(t, hook.URL, destination)
		require.Equal(t, event.ExternalID, id)
		require.Equal(t, []byte(event.Payload), payload)
		require.Equal(t, [][]byte{bytes.Repeat([]byte{3}, 32)}, secrets)
		return webhook.Result{StatusCode: 204}
	}}
	for range 2 {
		status, err := executeSend(t, a, input)
		require.NoError(t, err)
		require.Equal(t, enums.DeliveryStatusSucceeded, status)
	}
	require.Equal(t, 1, calls)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM webhook_attempts WHERE organization_id = $1 AND finished_at IS NOT NULL AND http_status = 204`, input.OrganizationID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSendReloadsDestinationAndRotation(t *testing.T) {
	t.Parallel()
	db, input, hook, _ := fixture(t)
	org, err := organizations.Get(t.Context(), db, input.OrganizationID)
	require.NoError(t, err)
	secret, err := encrypt.ForOrganization(testKeys{}, org.ExternalID).Seal(t.Context(), bytes.Repeat([]byte{4}, 32), encrypt.Binding{Purpose: "webhook-signing-secret", RecordID: hook.ExternalID})
	require.NoError(t, err)
	_, err = webhooks.RotateSecret(t.Context(), db, input.OrganizationID, hook.ExternalID, secret, time.Now().Add(time.Hour))
	require.NoError(t, err)
	_, err = webhooks.Update(t.Context(), db, input.OrganizationID, hook.ExternalID, &webhooks.UpdateOptions{URL: optional.Set("https://example.com/changed")})
	require.NoError(t, err)
	a := Activities{DB: db, Keys: testKeys{}, send: func(_ context.Context, destination, _ string, _ []byte, secrets [][]byte) webhook.Result {
		require.Equal(t, "https://example.com/changed", destination)
		require.Equal(t, [][]byte{bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{3}, 32)}, secrets)
		return webhook.Result{StatusCode: 200}
	}}
	status, err := executeSend(t, a, input)
	require.NoError(t, err)
	require.Equal(t, enums.DeliveryStatusSucceeded, status)
}

func TestSendFinishedFailureNotRepeated(t *testing.T) {
	t.Parallel()
	db, input, _, _ := fixture(t)
	calls := 0
	a := Activities{DB: db, Keys: testKeys{}, send: func(context.Context, string, string, []byte, [][]byte) webhook.Result {
		calls++
		return webhook.Result{StatusCode: 503}
	}}
	for range 2 {
		status, err := executeSend(t, a, input)
		require.NoError(t, err)
		require.Equal(t, enums.DeliveryStatusDelivering, status)
	}
	require.Equal(t, 1, calls)
}

func TestSendUnavailableAndExpired(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"disabled", "deleted organization", "expired"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			db, input, hook, _ := fixture(t)
			want := enums.DeliveryStatusCanceled
			switch change {
			case "disabled":
				_, err := webhooks.Update(t.Context(), db, input.OrganizationID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(false)})
				require.NoError(t, err)
			case "deleted organization":
				_, err := db.Exec(t.Context(), `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, input.OrganizationID)
				require.NoError(t, err)
			case "expired":
				_, err := db.Exec(t.Context(), `UPDATE webhook_deliveries SET created_at = $2 WHERE external_id = $1`, input.DeliveryID, time.Now().Add(-25*time.Hour))
				require.NoError(t, err)
				want = enums.DeliveryStatusFailed
			}
			a := Activities{DB: db, Keys: testKeys{}, send: func(context.Context, string, string, []byte, [][]byte) webhook.Result {
				t.Error("unavailable delivery sent HTTP")
				return webhook.Result{StatusCode: 200}
			}}
			status, err := executeSend(t, a, input)
			require.NoError(t, err)
			require.Equal(t, want, status)
			var persisted enums.DeliveryStatus
			require.NoError(t, db.QueryRow(t.Context(), `SELECT status FROM webhook_deliveries WHERE external_id = $1`, input.DeliveryID).Scan(&persisted))
			require.Equal(t, want, persisted)
		})
	}
}

func TestCancellationWinsResponse(t *testing.T) {
	t.Parallel()
	db, input, hook, _ := fixture(t)
	a := Activities{DB: db, Keys: testKeys{}, send: func(ctx context.Context, _, _ string, _ []byte, _ [][]byte) webhook.Result {
		_, err := webhooks.Update(ctx, db, input.OrganizationID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(false)})
		require.NoError(t, err)
		return webhook.Result{StatusCode: 200}
	}}
	status, err := executeSend(t, a, input)
	require.NoError(t, err)
	require.Equal(t, enums.DeliveryStatusCanceled, status)
}

func TestSendSanitizesEncryptionFailure(t *testing.T) {
	t.Parallel()
	db, input, _, _ := fixture(t)
	_, err := db.Exec(t.Context(), `UPDATE webhooks SET signing_secret = $2 WHERE organization_id = $1`, input.OrganizationID, []byte("PRIVATE-SECRET"))
	require.NoError(t, err)
	_, err = executeSend(t, Activities{DB: db, Keys: testKeys{}}, input)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "PRIVATE-SECRET")
	require.NotContains(t, err.Error(), "example.com")
	body, marshalErr := json.Marshal(input)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(body), "secret")
	require.NotContains(t, string(body), "payload")
}
