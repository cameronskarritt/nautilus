package webhooks_test

import (
	"testing"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestPruneUndeliveredEvents(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	firstOrg := testutil.CreateTestOrg(t, db, "prune-first", "First")
	secondOrg := testutil.CreateTestOrg(t, db, "prune-second", "Second")
	cutoff := time.Now().Add(-webhooks.Retention)
	for _, orgID := range []int{firstOrg, secondOrg} {
		eventID, _ := createEvent(t, db, orgID)
		_, err := db.Exec(ctx, `UPDATE events SET created_at = $2 WHERE id = $1`, eventID, cutoff.Add(-time.Hour))
		require.NoError(t, err)
	}
	freshID, _ := createEvent(t, db, firstOrg)
	count, err := webhooks.Prune(ctx, db, cutoff, 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var remaining int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&remaining))
	require.Equal(t, 2, remaining)
	count, err = webhooks.Prune(ctx, db, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var id int
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM events`).Scan(&id))
	require.Equal(t, freshID, id)
	count, err = webhooks.Prune(ctx, db, cutoff, 0)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestPrunePreservesActiveDeliveries(t *testing.T) {
	t.Parallel()
	for _, status := range []enums.DeliveryStatus{enums.DeliveryStatusPending, enums.DeliveryStatusDelivering} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := t.Context()
			orgID := testutil.CreateTestOrg(t, db, "active", "Webhooks")
			createHook(t, db, orgID)
			eventID, _ := createEvent(t, db, orgID)
			deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
			require.NoError(t, err)
			if status == enums.DeliveryStatusDelivering {
				_, err = webhooks.MarkDelivering(ctx, db, orgID, deliveries[0].ID)
				require.NoError(t, err)
			}
			cutoff := time.Now().Add(-webhooks.Retention)
			_, err = db.Exec(ctx, `UPDATE events SET created_at = $2 WHERE id = $1`, eventID, cutoff.Add(-time.Hour))
			require.NoError(t, err)
			count, err := webhooks.Prune(ctx, db, cutoff, 100)
			require.NoError(t, err)
			require.Zero(t, count)
			current, err := webhooks.GetDelivery(ctx, db, orgID, deliveries[0].ExternalID)
			require.NoError(t, err)
			require.NotNil(t, current)
			require.Equal(t, status, current.Status)
		})
	}
}

func TestPruneReplayHistoryIsAtomic(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "history", "Webhooks")
	hook := createHook(t, db, orgID)
	eventID, eventUUID := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	original := deliveries[0]
	replay, err := webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay", optional.Set(original.ExternalID))
	require.NoError(t, err)
	next, err := webhooks.Replay(ctx, db, orgID, eventUUID, hook.ExternalID, "replay-again", optional.Set(replay.ExternalID))
	require.NoError(t, err)
	for _, delivery := range []*webhooks.Delivery{original, replay, next} {
		attempt, err := webhooks.StartAttempt(ctx, db, orgID, delivery.ID, "1", hook.URL)
		require.NoError(t, err)
		_, err = webhooks.FinishAttempt(ctx, db, orgID, delivery.ID, attempt.ID, 200, "")
		require.NoError(t, err)
		_, err = webhooks.CompleteDelivery(ctx, db, orgID, delivery.ID, enums.DeliveryStatusSucceeded)
		require.NoError(t, err)
	}
	cutoff := time.Now().Add(-webhooks.Retention)
	_, err = db.Exec(ctx, `UPDATE events SET created_at = $2 WHERE id = $1`, eventID, cutoff.Add(-time.Hour))
	require.NoError(t, err)
	rollback := errors.New("rollback retention")
	err = database.Transact(ctx, db, func(tx database.Database) error {
		count, err := webhooks.Prune(ctx, tx, cutoff, 100)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	var savedAttempts, savedDeliveries int
	require.NoError(t, db.QueryRow(ctx, `SELECT (SELECT count(*) FROM webhook_attempts), (SELECT count(*) FROM webhook_deliveries)`).Scan(&savedAttempts, &savedDeliveries))
	require.Equal(t, 3, savedAttempts)
	require.Equal(t, 3, savedDeliveries)
	count, err := webhooks.Prune(ctx, db, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var events, attempts, history int
	require.NoError(t, db.QueryRow(ctx, `SELECT (SELECT count(*) FROM events), (SELECT count(*) FROM webhook_deliveries), (SELECT count(*) FROM webhook_attempts)`).Scan(&events, &history, &attempts))
	require.Zero(t, events)
	require.Zero(t, history)
	require.Zero(t, attempts)
}

func TestPruneSkipsReplayInProgress(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDBWithCommit(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "replay-lock", "Webhooks")
	hook := createHook(t, db, orgID)
	eventID, eventUUID := createEvent(t, db, orgID)
	deliveries, err := webhooks.CreateDeliveries(ctx, db, orgID, eventID, enums.WebhookEventTypeDocumentAvailable)
	require.NoError(t, err)
	_, err = webhooks.CompleteDelivery(ctx, db, orgID, deliveries[0].ID, enums.DeliveryStatusSucceeded)
	require.NoError(t, err)
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	replay, err := webhooks.Replay(ctx, tx, orgID, eventUUID, hook.ExternalID, "in-progress", optional.Set(deliveries[0].ExternalID))
	require.NoError(t, err)
	require.NotNil(t, replay)
	cutoff := time.Now().Add(time.Hour)
	count, err := webhooks.Prune(ctx, db, cutoff, 100)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, tx.Commit(ctx))
	count, err = webhooks.Prune(ctx, db, cutoff, 100)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = webhooks.CompleteDelivery(ctx, db, orgID, replay.ID, enums.DeliveryStatusSucceeded)
	require.NoError(t, err)
	count, err = webhooks.Prune(ctx, db, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
