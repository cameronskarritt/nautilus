package database_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nautilus/internal/database"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestWebhookSchemaStorage(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "webhook-storage", "Webhooks")
	secret := []byte{0, 255, 1, 128}
	var webhookID int
	var externalID string
	var enabled bool
	var eventTypes []string
	var storedSecret, previousSecret []byte
	var expiresAt, deletedAt sql.NullTime
	var createdAt, updatedAt time.Time
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO webhooks(organization_id, name, url, signing_secret)
		VALUES ($1, 'Orders', 'https://example.com/hook', $2)
		RETURNING id, external_id, event_types, enabled, signing_secret, previous_signing_secret,
			previous_secret_expires_at, created_at, updated_at, deleted_at`, orgID, secret,
	).Scan(&webhookID, &externalID, &eventTypes, &enabled, &storedSecret, &previousSecret,
		&expiresAt, &createdAt, &updatedAt, &deletedAt))
	require.Positive(t, webhookID)
	require.NotEmpty(t, externalID)
	require.Empty(t, eventTypes)
	require.True(t, enabled)
	require.Equal(t, secret, storedSecret)
	require.Nil(t, previousSecret)
	require.False(t, expiresAt.Valid)
	require.False(t, deletedAt.Valid)
	require.False(t, createdAt.IsZero())
	require.Equal(t, createdAt, updatedAt)

	expires := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.QueryRow(ctx, `UPDATE webhooks SET previous_signing_secret = $2,
		previous_secret_expires_at = $3, event_types = '{document.available}' WHERE id = $1
		RETURNING previous_signing_secret, previous_secret_expires_at, event_types`, webhookID, secret, expires,
	).Scan(&previousSecret, &expiresAt, &eventTypes))
	require.Equal(t, secret, previousSecret)
	require.True(t, expiresAt.Time.Equal(expires))
	require.Equal(t, []string{"document.available"}, eventTypes)

	payload := `{"id":"evt-test","type":"document.available","schema_version":1,"data":{"id":"doc-test"}}`
	var eventID int
	var storedPayload []byte
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO events(organization_id, type, schema_version, idempotency_key, payload, occurred_at)
		VALUES ($1, 'document.available', 1, 'document:1', $2, $3) RETURNING id, payload`, orgID, payload, expires,
	).Scan(&eventID, &storedPayload))
	require.JSONEq(t, payload, string(storedPayload))

	var deliveryID int
	var status string
	var completedAt sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, request_key)
		VALUES ($1, $2, $3, 'event', 'original') RETURNING id, status, completed_at`, orgID, webhookID, eventID,
	).Scan(&deliveryID, &status, &completedAt))
	require.Equal(t, "pending", status)
	require.False(t, completedAt.Valid)
	for _, key := range []string{"replay-1", "replay-2"} {
		_, err := db.Exec(ctx, `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, replayed_id, request_key)
			VALUES ($1, $2, $3, 'replay', $4, $5)`, orgID, webhookID, eventID, deliveryID, key)
		require.NoError(t, err)
	}
	var startedAt time.Time
	var finishedAt sql.NullTime
	var httpStatus sql.NullInt64
	var errorCode sql.NullString
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO webhook_attempts(organization_id, delivery_id, attempt_key, url)
		VALUES ($1, $2, 'attempt-1', 'https://example.com/hook') RETURNING started_at, finished_at, http_status, error_code`, orgID, deliveryID,
	).Scan(&startedAt, &finishedAt, &httpStatus, &errorCode))
	require.False(t, startedAt.IsZero())
	require.False(t, finishedAt.Valid)
	require.False(t, httpStatus.Valid)
	require.False(t, errorCode.Valid)
}

func TestWebhookSchemaConstraints(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		args  []int
		code  string
	}{
		{"webhook public identity unique", `UPDATE webhooks SET external_id = (SELECT external_id FROM webhooks WHERE id = $2) WHERE id = $1`, []int{1, 5}, "23505"},
		{"event public identity unique", `UPDATE events SET external_id = (SELECT external_id FROM events WHERE id = $2) WHERE id = $1`, []int{2, 6}, "23505"},
		{"delivery public identity unique", `UPDATE webhook_deliveries SET external_id = (SELECT external_id FROM webhook_deliveries WHERE id = $2) WHERE id = $1`, []int{3, 7}, "23505"},
		{"webhook organization", `UPDATE webhooks SET organization_id = -1 WHERE id = $1`, []int{1}, "23503"},
		{"event organization", `UPDATE events SET organization_id = -1 WHERE id = $1`, []int{2}, "23503"},
		{"webhook secret required", `UPDATE webhooks SET signing_secret = NULL WHERE id = $1`, []int{1}, "23502"},
		{"event key unique in organization", `INSERT INTO events(organization_id, type, schema_version, idempotency_key, payload, occurred_at) SELECT organization_id, type, schema_version, idempotency_key, payload, occurred_at FROM events WHERE id = $1`, []int{2}, "23505"},
		{"normal delivery unique", `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, request_key) SELECT organization_id, webhook_id, event_id, trigger, 'duplicate-event' FROM webhook_deliveries WHERE id = $1`, []int{3}, "23505"},
		{"replay request key unique", `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, request_key) SELECT organization_id, webhook_id, event_id, 'replay', request_key FROM webhook_deliveries WHERE id = $1`, []int{3}, "23505"},
		{"webhook tenant boundary", `UPDATE webhook_deliveries SET webhook_id = $2 WHERE id = $1`, []int{3, 5}, "23503"},
		{"event tenant boundary", `UPDATE webhook_deliveries SET event_id = $2 WHERE id = $1`, []int{3, 6}, "23503"},
		{"replay tenant boundary", `UPDATE webhook_deliveries SET replayed_id = $2 WHERE id = $1`, []int{3, 7}, "23503"},
		{"attempt tenant boundary", `INSERT INTO webhook_attempts(organization_id, delivery_id, attempt_key, url) VALUES ($1, $2, 'cross-tenant', 'https://example.com/hook')`, []int{4, 3}, "23503"},
		{"attempt key unique per delivery", `INSERT INTO webhook_attempts(organization_id, delivery_id, attempt_key, url) VALUES ($1, $2, 'attempt-1', 'https://example.com/hook')`, []int{0, 3}, "23505"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			first := createWebhookSchemaFixture(t, db, "first")
			second := createWebhookSchemaFixture(t, db, "second")
			ids := append(first, second...)
			args := make([]any, len(tt.args))
			for i, index := range tt.args {
				args[i] = ids[index]
			}
			_, err := db.Exec(t.Context(), tt.query, args...)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, tt.code, pgErr.Code)
		})
	}
}

func createWebhookSchemaFixture(t *testing.T, db database.Database, slug string) []int {
	t.Helper()
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, slug, "Webhooks")
	var webhookID, eventID, deliveryID int
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO webhooks(organization_id, name, url, signing_secret)
		VALUES ($1, 'Orders', 'https://example.com/hook', 'encrypted') RETURNING id`, orgID).Scan(&webhookID))
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO events(organization_id, type, schema_version, idempotency_key, payload, occurred_at)
		VALUES ($1, 'document.available', 1, 'document:1', '{}', CURRENT_TIMESTAMP) RETURNING id`, orgID).Scan(&eventID))
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, request_key)
		VALUES ($1, $2, $3, 'event', 'original') RETURNING id`, orgID, webhookID, eventID).Scan(&deliveryID))
	_, err := db.Exec(ctx, `INSERT INTO webhook_attempts(organization_id, delivery_id, attempt_key, url)
		VALUES ($1, $2, 'attempt-1', 'https://example.com/hook')`, orgID, deliveryID)
	require.NoError(t, err)
	return []int{orgID, webhookID, eventID, deliveryID}
}
