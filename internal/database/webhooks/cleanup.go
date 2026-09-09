package webhooks

import (
	"context"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

// Prune is internal maintenance across all organizations. It removes a bounded
// batch of old events and their history, preserving events with active deliveries.
func Prune(ctx context.Context, db database.Database, before time.Time, limit int) (int, error) {
	if before.IsZero() {
		return 0, ErrInvalidOptions
	}
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 1000)
	var count int
	err := database.Transact(ctx, db, func(tx database.Database) error {
		rows, err := tx.Query(ctx, `SELECT e.id FROM events e
   WHERE e.created_at < $1 AND NOT EXISTS (
    SELECT 1 FROM webhook_deliveries d WHERE d.organization_id = e.organization_id AND d.event_id = e.id AND d.status IN ($3,$4))
   ORDER BY e.created_at, e.id LIMIT $2 FOR UPDATE OF e SKIP LOCKED`, before, limit, enums.DeliveryStatusPending, enums.DeliveryStatusDelivering)
		if err != nil {
			return errors.Wrap(err, "unable to select expired webhook events")
		}
		ids := []int{}
		if err := database.ScanRows(rows, func(row database.Row) error {
			var id int
			if err := row.Scan(&id); err != nil {
				return errors.Wrap(err, "unable to read expired webhook event")
			}
			ids = append(ids, id)
			return nil
		}); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		// Foreign-key inserts take a key-share lock on the event. Recheck after our
		// exclusive lock so a replay committed during candidate selection is retained.
		rows, err = tx.Query(ctx, `SELECT e.id FROM events e WHERE e.id = ANY($1) AND NOT EXISTS (
   SELECT 1 FROM webhook_deliveries d WHERE d.organization_id = e.organization_id AND d.event_id = e.id AND d.status IN ($2,$3))`,
			ids, enums.DeliveryStatusPending, enums.DeliveryStatusDelivering)
		if err != nil {
			return errors.Wrap(err, "unable to recheck expired webhook events")
		}
		eligible := []int{}
		if err := database.ScanRows(rows, func(row database.Row) error {
			var id int
			if err := row.Scan(&id); err != nil {
				return errors.Wrap(err, "unable to read expired webhook event")
			}
			eligible = append(eligible, id)
			return nil
		}); err != nil {
			return err
		}
		if len(eligible) == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `DELETE FROM webhook_attempts a USING webhook_deliveries d
   WHERE a.organization_id = d.organization_id AND a.delivery_id = d.id AND d.event_id = ANY($1)`, eligible)
		if err != nil {
			return errors.Wrap(err, "unable to prune webhook attempts")
		}
		_, err = tx.Exec(ctx, `DELETE FROM webhook_deliveries WHERE event_id = ANY($1)`, eligible)
		if err != nil {
			return errors.Wrap(err, "unable to prune webhook deliveries")
		}
		result, err := tx.Exec(ctx, `DELETE FROM events WHERE id = ANY($1)`, eligible)
		if err != nil {
			return errors.Wrap(err, "unable to prune webhook events")
		}
		count = int(result.RowsAffected())
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
