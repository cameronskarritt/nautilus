package webhooks

import (
	"context"
	"database/sql"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

// CreateTestDelivery targets one endpoint regardless of its subscriptions.
func CreateTestDelivery(ctx context.Context, db database.Database, orgID, eventID int, webhookID, deliveryID string) (*Delivery, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	hookUUID, err := uuid.Parse(webhookID)
	if err != nil {
		return nil, ErrInvalidOptions
	}
	deliveryUUID, err := uuid.Parse(deliveryID)
	if err != nil || eventID <= 0 {
		return nil, ErrInvalidOptions
	}
	var result *Delivery
	err = database.Transact(ctx, db, func(tx database.Database) error {
		var id int
		var enabled bool
		err := tx.QueryRow(ctx, `SELECT id, enabled FROM webhooks WHERE organization_id = $1 AND external_id = $2
			AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) FOR UPDATE`, orgID, hookUUID.String()).Scan(&id, &enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "unable to lock test webhook")
		}
		result, err = GetDelivery(ctx, tx, orgID, deliveryUUID.String())
		if err != nil {
			return err
		}
		if result != nil {
			if result.WebhookID != id || result.EventID != eventID || result.Trigger != enums.DeliveryTriggerEvent {
				return ErrConflict
			}
			return nil
		}
		status := enums.DeliveryStatusPending
		if !enabled {
			status = enums.DeliveryStatusCanceled
		}
		_, err = tx.Exec(ctx, `INSERT INTO webhook_deliveries(external_id, organization_id, webhook_id, event_id, trigger, request_key, status, completed_at)
			SELECT $1, $2, $3, id, $5, $6, $8, CASE WHEN $9 THEN NULL ELSE CURRENT_TIMESTAMP END FROM webhook_events WHERE organization_id = $2 AND id = $4 AND type = $7
			ON CONFLICT (organization_id, request_key) DO NOTHING`, deliveryUUID.String(), orgID, id, eventID,
			enums.DeliveryTriggerEvent, "test:"+deliveryUUID.String(), enums.WebhookEventTypeWebhookTest, status, enabled)
		if err != nil {
			return errors.Wrap(err, "unable to create test delivery")
		}
		result, err = GetDelivery(ctx, tx, orgID, deliveryUUID.String())
		return err
	})
	return result, err
}
