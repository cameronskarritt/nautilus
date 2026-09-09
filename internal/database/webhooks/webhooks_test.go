package webhooks_test

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestWebhookLifecycle(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "lifecycle", "Webhooks")
	hook := createHook(t, db, orgID)
	require.Equal(t, []enums.WebhookEventType{enums.WebhookEventTypeDocumentAvailable}, hook.EventTypes)
	require.True(t, hook.Enabled)
	otherID := testutil.CreateTestOrg(t, db, "other", "Other")
	missing, err := webhooks.GetByExternalID(ctx, db, otherID, hook.ExternalID)
	require.NoError(t, err)
	require.Nil(t, missing)
	missing, err = webhooks.GetByExternalID(ctx, db, orgID, "invalid")
	require.NoError(t, err)
	require.Nil(t, missing)
	hook, err = webhooks.Update(ctx, db, orgID, hook.ExternalID, &webhooks.UpdateOptions{Name: optional.Set("  Renamed  "), URL: optional.Set("https://example.com/changed"), EventTypes: optional.Set([]enums.WebhookEventType{enums.WebhookEventTypeWebhookTest, enums.WebhookEventTypeDocumentAvailable, enums.WebhookEventTypeWebhookTest})})
	require.NoError(t, err)
	require.Equal(t, "Renamed", hook.Name)
	require.Equal(t, "https://example.com/changed", hook.URL)
	require.Equal(t, []enums.WebhookEventType{enums.WebhookEventTypeDocumentAvailable, enums.WebhookEventTypeWebhookTest}, hook.EventTypes)
	expires := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	rotated, err := webhooks.RotateSecret(ctx, db, orgID, hook.ExternalID, []byte("new-encrypted"), expires)
	require.NoError(t, err)
	require.Equal(t, []byte("new-encrypted"), rotated.SigningSecret)
	require.Equal(t, []byte("encrypted"), rotated.PreviousSigningSecret.Data)
	require.True(t, expires.Equal(rotated.PreviousSecretExpiresAt.Data))
	_, err = webhooks.RotateSecret(ctx, db, orgID, hook.ExternalID, []byte("third-encrypted"), expires)
	require.ErrorIs(t, err, webhooks.ErrConflict)

	data, err := json.Marshal(rotated)
	require.NoError(t, err)
	require.NotContains(t, string(data), "encrypted")
	require.NotContains(t, string(data), "secret")
	require.NotContains(t, string(data), "organization_id")
	deleted, err := webhooks.Delete(ctx, db, otherID, hook.ExternalID)
	require.NoError(t, err)
	require.False(t, deleted)
	deleted, err = webhooks.Delete(ctx, db, orgID, hook.ExternalID)
	require.NoError(t, err)
	require.True(t, deleted)
	missing, err = webhooks.Get(ctx, db, orgID, hook.ID)
	require.NoError(t, err)
	require.Nil(t, missing)
	page, err := webhooks.List(ctx, db, orgID, pagination.Params{})
	require.NoError(t, err)
	require.Empty(t, page.Data)
}

func TestWebhookValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		mutate func(*webhooks.CreateOptions)
		want   error
	}{
		{"name", func(o *webhooks.CreateOptions) { o.Name = " " }, webhooks.ErrInvalidName},
		{"empty types", func(o *webhooks.CreateOptions) { o.EventTypes = nil }, webhooks.ErrInvalidEventTypes},
		{"unknown type", func(o *webhooks.CreateOptions) { o.EventTypes = []enums.WebhookEventType{"*"} }, webhooks.ErrInvalidEventTypes},
		{"secret", func(o *webhooks.CreateOptions) { o.SigningSecret = nil }, webhooks.ErrInvalidSecret},
		{"external ID", func(o *webhooks.CreateOptions) { o.ExternalID = "invalid" }, webhooks.ErrInvalidOptions},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := hookOptions()
			tt.mutate(opts)
			_, err := webhooks.Create(t.Context(), nil, 1, opts)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestWebhookEndpointLimit(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDBWithCommit(t)
	orgID := testutil.CreateTestOrg(t, db, "limit", "Webhooks")
	var wg sync.WaitGroup
	results := make(chan error, webhooks.MaxEndpoints+2)
	for range webhooks.MaxEndpoints + 2 {
		wg.Go(func() { _, err := webhooks.Create(t.Context(), db, orgID, hookOptions()); results <- err })
	}
	wg.Wait()
	close(results)
	var created, limited int
	for err := range results {
		if err == nil {
			created++
		} else {
			require.ErrorIs(t, err, webhooks.ErrLimit)
			limited++
		}
	}
	require.Equal(t, webhooks.MaxEndpoints, created)
	require.Equal(t, 2, limited)
	page, err := webhooks.List(t.Context(), db, orgID, pagination.Params{})
	require.NoError(t, err)
	_, err = webhooks.Delete(t.Context(), db, orgID, page.Data[0].ExternalID)
	require.NoError(t, err)
	createHook(t, db, orgID)
}

func TestDeliveryFanoutAndCancellation(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"disable", "delete"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := t.Context()
			orgID := testutil.CreateTestOrg(t, db, "fanout", "Webhooks")
			hook := createHook(t, db, orgID)
			ignored := createHook(t, db, orgID)
			_, err := webhooks.Update(ctx, db, orgID, ignored.ExternalID, &webhooks.UpdateOptions{EventTypes: optional.Set([]enums.WebhookEventType{enums.WebhookEventTypeWebhookTest})})
			require.NoError(t, err)
			eventID, _ := createEvent(t, db, orgID)
			deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
			require.NoError(t, err)
			require.Len(t, deliveries, 1)
			delivery := deliveries[0]
			require.Equal(t, hook.ID, delivery.WebhookID)
			again, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
			require.NoError(t, err)
			require.Len(t, again, 1)
			require.Equal(t, delivery.ID, again[0].ID)
			changed, err := webhooks.MarkDelivering(ctx, db, orgID, delivery.ID)
			require.NoError(t, err)
			require.True(t, changed)
			if action == "disable" {
				_, err = webhooks.Update(ctx, db, orgID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(false)})
				require.NoError(t, err)
				_, err = webhooks.Update(ctx, db, orgID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(true)})
			} else {
				_, err = webhooks.Delete(ctx, db, orgID, hook.ExternalID)
			}
			require.NoError(t, err)
			var status enums.DeliveryStatus
			var completed time.Time
			require.NoError(t, db.QueryRow(ctx, `SELECT status, completed_at FROM webhook_deliveries WHERE id = $1`, delivery.ID).Scan(&status, &completed))
			require.Equal(t, enums.DeliveryStatusCanceled, status)
			require.False(t, completed.IsZero())
			changed, err = webhooks.CompleteDelivery(ctx, db, orgID, delivery.ID, enums.DeliveryStatusSucceeded)
			require.NoError(t, err)
			require.False(t, changed)
			attempt, err := webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "late", hook.URL)
			require.NoError(t, err)
			require.Nil(t, attempt)
		})
	}
}

func TestReplayIdempotency(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "replay", "Webhooks")
	hook := createHook(t, db, orgID)
	eventID, eventUUID := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	replay, err := webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-1", optional.Set(deliveries[0].ExternalID))
	require.NoError(t, err)
	require.Equal(t, enums.DeliveryTriggerReplay, replay.Trigger)
	require.Equal(t, deliveries[0].ExternalID, replay.ReplayedExternalID.Data)
	again, err := webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-1", optional.Set(deliveries[0].ExternalID))
	require.NoError(t, err)
	require.Equal(t, replay.ID, again.ID)
	_, err = webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-1", optional.Empty[string]())
	require.ErrorIs(t, err, webhooks.ErrConflict)
	otherHook := createHook(t, db, orgID)
	_, err = webhooks.Replay(ctx, db, orgID, eventUUID, otherHook.ExternalID, "replay-2", optional.Set(deliveries[0].ExternalID))
	require.ErrorIs(t, err, webhooks.ErrConflict)
	otherOrg := testutil.CreateTestOrg(t, db, "other", "Other")
	missing, err := webhooks.Replay(ctx, db, otherOrg, eventUUID, hook.ExternalID, "replay-3", optional.Empty[string]())
	require.NoError(t, err)
	require.Nil(t, missing)
	normals, err := webhooks.ListForEvent(ctx, db, orgID, eventID)
	require.NoError(t, err)
	require.Len(t, normals, 1)
	_, err = db.Exec(ctx, `UPDATE events SET created_at = $2 WHERE id = $1`, eventID, time.Now().Add(-webhooks.Retention-time.Hour))
	require.NoError(t, err)
	missing, err = webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "expired", optional.Empty[string]())
	require.NoError(t, err)
	require.Nil(t, missing)
	again, err = webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-1", optional.Set(deliveries[0].ExternalID))
	require.NoError(t, err)
	require.Equal(t, replay.ID, again.ID)
	_, err = webhooks.Update(ctx, db, orgID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(false)})
	require.NoError(t, err)
	again, err = webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-1", optional.Set(deliveries[0].ExternalID))
	require.NoError(t, err)
	require.Equal(t, replay.ID, again.ID)
	require.Equal(t, enums.DeliveryStatusCanceled, again.Status)
	_, err = webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-1", optional.Empty[string]())
	require.ErrorIs(t, err, webhooks.ErrConflict)
}

func TestAttemptsAndTerminalStatus(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "attempts", "Webhooks")
	hook := createHook(t, db, orgID)
	eventID, _ := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	delivery := deliveries[0]
	attempt, err := webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "attempt-1", hook.URL)
	require.NoError(t, err)
	_, err = uuid.Parse(attempt.ExternalID)
	require.NoError(t, err)
	body, err := json.Marshal(attempt)
	require.NoError(t, err)
	var public map[string]any
	require.NoError(t, json.Unmarshal(body, &public))
	require.Equal(t, attempt.ExternalID, public["id"])
	require.NotContains(t, public, "organization_id")
	require.NotContains(t, public, "delivery_id")
	require.NotContains(t, public, "attempt_key")
	require.False(t, attempt.FinishedAt.Set)
	again, err := webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "attempt-1", hook.URL)
	require.NoError(t, err)
	require.Equal(t, attempt.ID, again.ID)
	require.Equal(t, attempt.StartedAt, again.StartedAt)
	_, err = webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "attempt-1", "https://example.com/changed")
	require.ErrorIs(t, err, webhooks.ErrConflict)
	finished, err := webhooks.FinishAttempt(ctx, db, orgID, delivery.ID, attempt.ID, 0, enums.WebhookErrorTimeout)
	require.NoError(t, err)
	require.True(t, finished.FinishedAt.Set)
	require.False(t, finished.HTTPStatus.Set)
	require.Equal(t, enums.WebhookErrorTimeout, finished.ErrorCode.Data)
	repeated, err := webhooks.FinishAttempt(ctx, db, orgID, delivery.ID, attempt.ID, 200, "")
	require.NoError(t, err)
	require.Equal(t, finished, repeated)
	changed, err := webhooks.CompleteDelivery(ctx, db, orgID, delivery.ID, enums.DeliveryStatusSucceeded)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = webhooks.MarkDelivering(ctx, db, orgID, delivery.ID)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = webhooks.CompleteDelivery(ctx, db, orgID, delivery.ID, enums.DeliveryStatusFailed)
	require.NoError(t, err)
	require.False(t, changed)
	missing, err := webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "attempt-2", hook.URL)
	require.NoError(t, err)
	require.Nil(t, missing)
	page, err := webhooks.ListAttempts(ctx, db, orgID, hook.ExternalID, delivery.ExternalID, pagination.Params{})
	require.NoError(t, err)
	require.Len(t, page.Data, 1)
	require.Equal(t, finished, page.Data[0])
	otherID := testutil.CreateTestOrg(t, db, "other", "Other")
	missing, err = webhooks.FinishAttempt(ctx, db, otherID, delivery.ID, attempt.ID, 200, "")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestWebhookPaginationAndDeletedOrganization(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "pagination", "Webhooks")
	first := createHook(t, db, orgID)
	second := createHook(t, db, orgID)
	page, err := webhooks.List(ctx, db, orgID, pagination.Params{Limit: 1})
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, second.ID, page.Data[0].ID)
	cursor, err := pagination.Decode(page.NextCursor)
	require.NoError(t, err)
	page, err = webhooks.List(ctx, db, orgID, pagination.Params{Limit: 1, Cursor: cursor})
	require.NoError(t, err)
	require.False(t, page.HasMore)
	require.Equal(t, first.ID, page.Data[0].ID)
	_, err = webhooks.List(ctx, db, orgID+1, pagination.Params{Cursor: cursor})
	require.ErrorIs(t, err, webhooks.ErrInvalidCursor)
	_, err = webhooks.ListDeliveries(ctx, db, orgID, first.ExternalID, pagination.Params{Cursor: cursor})
	require.ErrorIs(t, err, webhooks.ErrInvalidCursor)
	cursor["extra"] = "invalid"
	_, err = webhooks.List(ctx, db, orgID, pagination.Params{Cursor: cursor})
	require.ErrorIs(t, err, webhooks.ErrInvalidCursor)
	eventID, eventUUID := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, orgID)
	require.NoError(t, err)
	missing, err := webhooks.GetByExternalID(ctx, db, orgID, first.ExternalID)
	require.NoError(t, err)
	require.Nil(t, missing)
	missing, err = webhooks.Create(ctx, db, orgID, hookOptions())
	require.NoError(t, err)
	require.Nil(t, missing)
	missing, err = webhooks.Update(ctx, db, orgID, first.ExternalID, &webhooks.UpdateOptions{Name: optional.Set("hidden")})
	require.NoError(t, err)
	require.Nil(t, missing)
	missing, err = webhooks.RotateSecret(ctx, db, orgID, first.ExternalID, []byte("new"), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, missing)
	hidden, err := webhooks.GetDelivery(ctx, db, orgID, deliveries[0].ExternalID)
	require.NoError(t, err)
	require.Nil(t, hidden)
	hidden, err = webhooks.Replay(ctx, db, orgID, eventUUID, first.ExternalID, "hidden", optional.Empty[string]())
	require.NoError(t, err)
	require.Nil(t, hidden)
	changed, err := webhooks.CompleteDelivery(ctx, db, orgID, deliveries[0].ID, enums.DeliveryStatusSucceeded)
	require.NoError(t, err)
	require.False(t, changed)
}

func hookOptions() *webhooks.CreateOptions {
	return &webhooks.CreateOptions{ExternalID: uuid.NewV4().String(), Name: "Orders", URL: "https://example.com/hook", EventTypes: []enums.WebhookEventType{enums.WebhookEventTypeDocumentAvailable, enums.WebhookEventTypeDocumentAvailable}, SigningSecret: []byte("encrypted")}
}

func createHook(t *testing.T, db database.Database, orgID int) *webhooks.Webhook {
	t.Helper()
	hook, err := webhooks.Create(t.Context(), db, orgID, hookOptions())
	require.NoError(t, err)
	require.NotNil(t, hook)
	return hook
}

func createEvent(t *testing.T, db database.Database, orgID int) (int, string) {
	t.Helper()
	var id int
	var externalID string
	err := db.QueryRow(t.Context(), `INSERT INTO events(organization_id, type, schema_version, idempotency_key, payload, occurred_at)
  VALUES ($1,$2,1,$3,'{}',CURRENT_TIMESTAMP) RETURNING id, external_id`, orgID, enums.WebhookEventTypeDocumentAvailable, strconv.FormatInt(time.Now().UnixNano(), 10)).Scan(&id, &externalID)
	require.NoError(t, err)
	return id, externalID
}

func TestDeliveryAndAttemptPagination(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "pages", "Webhooks")
	hook := createHook(t, db, orgID)
	eventID, eventUUID := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	original := deliveries[0]
	replay, err := webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay", optional.Set(original.ExternalID))
	require.NoError(t, err)
	page, err := webhooks.ListDeliveries(ctx, db, orgID, hook.ExternalID, pagination.Params{Limit: 1})
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, replay.ID, page.Data[0].ID)
	cursor, err := pagination.Decode(page.NextCursor)
	require.NoError(t, err)
	page, err = webhooks.ListDeliveries(ctx, db, orgID, hook.ExternalID, pagination.Params{Limit: 1, Cursor: cursor})
	require.NoError(t, err)
	require.False(t, page.HasMore)
	require.Equal(t, original.ID, page.Data[0].ID)
	other := createHook(t, db, orgID)
	_, err = webhooks.ListDeliveries(ctx, db, orgID, other.ExternalID, pagination.Params{Cursor: cursor})
	require.ErrorIs(t, err, webhooks.ErrInvalidCursor)
	first, err := webhooks.StartAttempt(ctx, db, orgID, original.ID, "1", hook.URL)
	require.NoError(t, err)
	second, err := webhooks.StartAttempt(ctx, db, orgID, original.ID, "2", hook.URL)
	require.NoError(t, err)
	attempts, err := webhooks.ListAttempts(ctx, db, orgID, hook.ExternalID, original.ExternalID, pagination.Params{Limit: 1})
	require.NoError(t, err)
	require.True(t, attempts.HasMore)
	require.Equal(t, second.ID, attempts.Data[0].ID)
	cursor, err = pagination.Decode(attempts.NextCursor)
	require.NoError(t, err)
	attempts, err = webhooks.ListAttempts(ctx, db, orgID, hook.ExternalID, original.ExternalID, pagination.Params{Limit: 1, Cursor: cursor})
	require.NoError(t, err)
	require.False(t, attempts.HasMore)
	require.Equal(t, first.ID, attempts.Data[0].ID)
	_, err = webhooks.ListAttempts(ctx, db, orgID, hook.ExternalID, replay.ExternalID, pagination.Params{Cursor: cursor})
	require.ErrorIs(t, err, webhooks.ErrInvalidCursor)
	attempts, err = webhooks.ListAttempts(ctx, db, orgID, other.ExternalID, original.ExternalID, pagination.Params{})
	require.NoError(t, err)
	require.Empty(t, attempts.Data)
}

func TestCancelUnavailable(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "unavailable", "Webhooks")
	otherID := testutil.CreateTestOrg(t, db, "other", "Other")
	createHook(t, db, orgID)
	eventID, _ := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	delivery := deliveries[0]
	changed, err := webhooks.CancelUnavailable(ctx, db, orgID, delivery.ExternalID)
	require.NoError(t, err)
	require.False(t, changed)
	_, err = db.Exec(ctx, `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, orgID)
	require.NoError(t, err)
	hidden, err := webhooks.GetDelivery(ctx, db, orgID, delivery.ExternalID)
	require.NoError(t, err)
	require.Nil(t, hidden)
	changed, err = webhooks.CancelUnavailable(ctx, db, otherID, delivery.ExternalID)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = webhooks.CancelUnavailable(ctx, db, orgID, delivery.ExternalID)
	require.NoError(t, err)
	require.True(t, changed)
	var status enums.DeliveryStatus
	var completedAt time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT status, completed_at FROM webhook_deliveries WHERE organization_id = $1 AND id = $2`, orgID, delivery.ID).Scan(&status, &completedAt))
	require.Equal(t, enums.DeliveryStatusCanceled, status)
	require.False(t, completedAt.IsZero())
	changed, err = webhooks.CancelUnavailable(ctx, db, orgID, delivery.ExternalID)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestCanceledAttemptCannotResume(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "resume", "Webhooks")
	hook := createHook(t, db, orgID)
	eventID, _ := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	delivery := deliveries[0]
	attempt, err := webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "interrupted", hook.URL)
	require.NoError(t, err)
	require.NotNil(t, attempt)
	_, err = webhooks.Update(ctx, db, orgID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(false)})
	require.NoError(t, err)
	_, err = webhooks.Update(ctx, db, orgID, hook.ExternalID, &webhooks.UpdateOptions{Enabled: optional.Set(true)})
	require.NoError(t, err)
	attempt, err = webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "interrupted", hook.URL)
	require.NoError(t, err)
	require.Nil(t, attempt)
}

func TestFinishAttemptRequiresOutcome(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		code   enums.WebhookErrorCode
	}{
		{name: "missing outcome"},
		{name: "conflicting outcome", status: 200, code: enums.WebhookErrorTimeout},
		{name: "invalid status", status: 99},
		{name: "unknown error", code: "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := webhooks.FinishAttempt(t.Context(), nil, 1, 1, 1, tt.status, tt.code)
			require.ErrorIs(t, err, webhooks.ErrInvalidOptions)
		})
	}
}
