package webhookevents_test

import (
	"encoding/json"
	"testing"
	"time"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRecordImmutable(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "event", "Events")
	doc, err := documents.Create(ctx, db, orgID, &documents.CreateOptions{Filename: "private.txt", ContentType: "text/plain", Size: 1})
	require.NoError(t, err)
	_, err = documents.MarkUploaded(ctx, db, orgID, doc.ExternalID)
	require.NoError(t, err)
	opts := &webhookevents.CreateOptions{Type: enums.WebhookEventTypeDocumentAvailable, IdempotencyKey: "document:available:1", OccurredAt: time.Now(), DocumentID: doc.ExternalID}
	event, created, err := webhookevents.Record(ctx, db, orgID, opts)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, event)
	var payload webhookevents.Payload
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	require.Equal(t, event.ExternalID, payload.ID)
	require.Equal(t, opts.Type, payload.Type)
	require.Equal(t, opts.DocumentID, payload.Data.DocumentID)
	require.NotEmpty(t, payload.OrganizationID)
	require.True(t, event.OccurredAt.Equal(payload.OccurredAt))

	opts.OccurredAt = opts.OccurredAt.Add(time.Hour)
	again, created, err := webhookevents.Record(ctx, db, orgID, opts)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, event, again)
	opts.DocumentID = uuid.NewV4().String()
	_, _, err = webhookevents.Record(ctx, db, orgID, opts)
	require.ErrorIs(t, err, webhookevents.ErrConflict)

	other := testutil.CreateTestOrg(t, db, "other-event", "Other")
	opts.DocumentID = doc.ExternalID
	foreign, created, err := webhookevents.Record(ctx, db, other, opts)
	require.NoError(t, err)
	require.False(t, created)
	require.Nil(t, foreign)
	missing, err := webhookevents.GetByExternalID(ctx, db, other, event.ExternalID)
	require.NoError(t, err)
	require.Nil(t, missing)
	missing, err = webhookevents.GetByKey(ctx, db, other, event.IdempotencyKey)
	require.NoError(t, err)
	require.Nil(t, missing)

	_, err = db.Exec(ctx, `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, orgID)
	require.NoError(t, err)
	missing, err = webhookevents.GetByExternalID(ctx, db, orgID, event.ExternalID)
	require.NoError(t, err)
	require.Nil(t, missing)
	missing, created, err = webhookevents.Record(ctx, db, orgID, opts)
	require.NoError(t, err)
	require.False(t, created)
	require.Nil(t, missing)
}

func TestRecordTransaction(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "rollback-event", "Events")
	opts := &webhookevents.CreateOptions{Type: enums.WebhookEventTypeWebhookTest, IdempotencyKey: "test", OccurredAt: time.Now()}
	err := database.Transact(ctx, db, func(tx database.Database) error {
		_, created, err := webhookevents.Record(ctx, tx, orgID, opts)
		require.NoError(t, err)
		require.True(t, created)
		return webhookevents.ErrConflict
	})
	require.ErrorIs(t, err, webhookevents.ErrConflict)
	event, err := webhookevents.GetByKey(ctx, db, orgID, "test")
	require.NoError(t, err)
	require.Nil(t, event)
}

func TestRecordValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opts *webhookevents.CreateOptions
	}{
		{"missing options", nil},
		{"unknown type", &webhookevents.CreateOptions{Type: "document.unknown", IdempotencyKey: "x", OccurredAt: time.Now()}},
		{"missing key", &webhookevents.CreateOptions{Type: enums.WebhookEventTypeWebhookTest, OccurredAt: time.Now()}},
		{"missing time", &webhookevents.CreateOptions{Type: enums.WebhookEventTypeWebhookTest, IdempotencyKey: "x"}},
		{"invalid document", &webhookevents.CreateOptions{Type: enums.WebhookEventTypeDocumentAvailable, IdempotencyKey: "x", OccurredAt: time.Now(), DocumentID: "no"}},
		{"test has document", &webhookevents.CreateOptions{Type: enums.WebhookEventTypeWebhookTest, IdempotencyKey: "x", OccurredAt: time.Now(), DocumentID: uuid.NewV4().String()}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := webhookevents.Record(t.Context(), nil, 1, tt.opts)
			require.ErrorIs(t, err, webhookevents.ErrInvalidEvent)
		})
	}
}
