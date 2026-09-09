package webhooks

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
)

func CreateDeliveries(ctx context.Context, db database.Database, orgID, eventID int, eventType enums.WebhookEventType) ([]*Delivery, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	if eventID <= 0 || !eventType.IsValid() {
		return nil, ErrInvalidOptions
	}
	err := database.Transact(ctx, db, func(tx database.Database) error {
		rows, err := tx.Query(ctx, `SELECT id FROM webhooks WHERE organization_id = $1 AND deleted_at IS NULL AND enabled
   AND $3 = ANY(event_types) AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND EXISTS (SELECT 1 FROM events WHERE organization_id = $1 AND id = $2 AND type = $3)
   ORDER BY id FOR UPDATE`, orgID, eventID, eventType)
		if err != nil {
			return errors.Wrap(err, "unable to select webhook subscribers")
		}
		ids := []int{}
		if err := database.ScanRows(rows, func(row database.Row) error {
			var id int
			if err := row.Scan(&id); err != nil {
				return errors.Wrap(err, "unable to read webhook subscriber")
			}
			ids = append(ids, id)
			return nil
		}); err != nil {
			return err
		}
		for _, id := range ids {
			_, err := tx.Exec(ctx, `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, request_key)
    VALUES ($1,$2,$3,'event',$4) ON CONFLICT (organization_id, webhook_id, event_id) WHERE trigger = 'event' DO NOTHING`,
				orgID, id, eventID, "event:"+strconv.Itoa(eventID)+":"+strconv.Itoa(id))
			if err != nil {
				return errors.Wrap(err, "unable to create webhook delivery")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ListForEvent(ctx, db, orgID, eventID)
}

func ListForEvent(ctx context.Context, db database.Database, orgID, eventID int) ([]*Delivery, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	rows, err := db.Query(ctx, `SELECT d.id, d.external_id, d.organization_id, d.webhook_id, d.event_id, w.external_id, e.external_id,
 d.trigger, d.replayed_id, r.external_id, d.request_key, d.status, d.created_at, d.updated_at, d.completed_at FROM webhook_deliveries d
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 JOIN events e ON e.organization_id = d.organization_id AND e.id = d.event_id
 LEFT JOIN webhook_deliveries r ON r.organization_id = d.organization_id AND r.id = d.replayed_id
 WHERE d.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND d.event_id = $2 AND d.trigger = $3 ORDER BY d.id`, orgID, eventID, enums.DeliveryTriggerEvent)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list event deliveries")
	}
	return scanDeliveries(rows)
}

func GetDelivery(ctx context.Context, db database.Database, orgID int, externalID string) (*Delivery, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	return scanDelivery(db.QueryRow(ctx, `SELECT d.id, d.external_id, d.organization_id, d.webhook_id, d.event_id, w.external_id, e.external_id,
 d.trigger, d.replayed_id, r.external_id, d.request_key, d.status, d.created_at, d.updated_at, d.completed_at FROM webhook_deliveries d
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 JOIN events e ON e.organization_id = d.organization_id AND e.id = d.event_id
 LEFT JOIN webhook_deliveries r ON r.organization_id = d.organization_id AND r.id = d.replayed_id
 WHERE d.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND d.external_id = $2`, orgID, id.String()))
}

func ListDeliveries(ctx context.Context, db database.Database, orgID int, webhookExternalID string, params pagination.Params) (pagination.Page[*Delivery], error) {
	if orgID <= 0 {
		return pagination.Page[*Delivery]{}, ErrInvalidOrganization
	}
	id, err := uuid.Parse(webhookExternalID)
	if err != nil {
		return pagination.Page[*Delivery]{Data: []*Delivery{}}, nil
	}
	scope := "deliveries:" + id.String()
	limit, before, err := pageParams(params, orgID, scope)
	if err != nil {
		return pagination.Page[*Delivery]{}, err
	}
	rows, err := db.Query(ctx, `SELECT d.id, d.external_id, d.organization_id, d.webhook_id, d.event_id, w.external_id, e.external_id,
 d.trigger, d.replayed_id, r.external_id, d.request_key, d.status, d.created_at, d.updated_at, d.completed_at FROM webhook_deliveries d
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 JOIN events e ON e.organization_id = d.organization_id AND e.id = d.event_id
 LEFT JOIN webhook_deliveries r ON r.organization_id = d.organization_id AND r.id = d.replayed_id
 WHERE d.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND w.external_id = $2 AND ($4::bigint = 0 OR d.id < $4) ORDER BY d.id DESC LIMIT $3`, orgID, id.String(), limit+1, before)
	if err != nil {
		return pagination.Page[*Delivery]{}, errors.Wrap(err, "unable to list webhook deliveries")
	}
	items, err := scanDeliveries(rows)
	if err != nil {
		return pagination.Page[*Delivery]{}, err
	}
	return pagination.Build(items, limit, func(item *Delivery) pagination.Cursor { return pageCursor(item.ID, orgID, scope) }), nil
}

func Replay(ctx context.Context, db database.Database, orgID int, eventExternalID, webhookExternalID, requestKey string, replayedExternalID optional.Optional[string]) (*Delivery, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	eventUUID, err := uuid.Parse(eventExternalID)
	if err != nil {
		return nil, nil
	}
	webhookUUID, err := uuid.Parse(webhookExternalID)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(requestKey) == "" || len(requestKey) > 255 || strings.HasPrefix(requestKey, "event:") {
		return nil, ErrInvalidOptions
	}
	if replayedExternalID.Set {
		id, err := uuid.Parse(replayedExternalID.Data)
		if err != nil {
			return nil, nil
		}
		replayedExternalID.Data = id.String()
	}
	var result *Delivery
	err = database.Transact(ctx, db, func(tx database.Database) error {
		// Serialize replay creation with endpoint disable/delete and normal fanout.
		var webhookID int
		var enabled bool
		err := tx.QueryRow(ctx, `SELECT id, enabled FROM webhooks WHERE organization_id = $1 AND external_id = $2
   AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) FOR UPDATE`, orgID, webhookUUID.String()).Scan(&webhookID, &enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "unable to lock replay webhook")
		}
		result, err = scanDelivery(tx.QueryRow(ctx, `SELECT d.id, d.external_id, d.organization_id, d.webhook_id, d.event_id, w.external_id, e.external_id,
 d.trigger, d.replayed_id, r.external_id, d.request_key, d.status, d.created_at, d.updated_at, d.completed_at FROM webhook_deliveries d
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 JOIN events e ON e.organization_id = d.organization_id AND e.id = d.event_id
 LEFT JOIN webhook_deliveries r ON r.organization_id = d.organization_id AND r.id = d.replayed_id
 WHERE d.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND d.request_key = $2`, orgID, requestKey))
		if err != nil {
			return err
		}
		if result != nil {
			if result.WebhookID != webhookID || result.EventExternalID != eventUUID.String() || result.Trigger != enums.DeliveryTriggerReplay || result.ReplayedExternalID != replayedExternalID {
				return ErrConflict
			}
			return nil
		}
		if !enabled {
			return nil
		}
		var eventID int
		err = tx.QueryRow(ctx, `SELECT id FROM events WHERE organization_id = $1 AND external_id = $2 AND created_at >= $3 FOR KEY SHARE`, orgID, eventUUID.String(), time.Now().Add(-Retention)).Scan(&eventID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "unable to read replay event")
		}
		lineage := optional.Empty[int]()
		if replayedExternalID.Set {
			var id int
			err := tx.QueryRow(ctx, `SELECT id FROM webhook_deliveries WHERE organization_id = $1 AND external_id = $2 AND webhook_id = $3 AND event_id = $4`, orgID, replayedExternalID.Data, webhookID, eventID).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			if err != nil {
				return errors.Wrap(err, "unable to read replay lineage")
			}
			lineage = optional.Set(id)
		}
		_, err = tx.Exec(ctx, `INSERT INTO webhook_deliveries(organization_id, webhook_id, event_id, trigger, replayed_id, request_key)
   VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (organization_id, request_key) DO NOTHING`, orgID, webhookID, eventID, enums.DeliveryTriggerReplay, lineage, requestKey)
		if err != nil {
			return errors.Wrap(err, "unable to create webhook replay")
		}
		result, err = scanDelivery(tx.QueryRow(ctx, `SELECT d.id, d.external_id, d.organization_id, d.webhook_id, d.event_id, w.external_id, e.external_id,
 d.trigger, d.replayed_id, r.external_id, d.request_key, d.status, d.created_at, d.updated_at, d.completed_at FROM webhook_deliveries d
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 JOIN events e ON e.organization_id = d.organization_id AND e.id = d.event_id
 LEFT JOIN webhook_deliveries r ON r.organization_id = d.organization_id AND r.id = d.replayed_id
 WHERE d.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND d.request_key = $2`, orgID, requestKey))
		if err != nil {
			return err
		}
		if result == nil || result.WebhookID != webhookID || result.EventID != eventID || result.Trigger != enums.DeliveryTriggerReplay || result.ReplayedID != lineage {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func MarkDelivering(ctx context.Context, db database.Database, orgID, deliveryID int) (bool, error) {
	if orgID <= 0 {
		return false, ErrInvalidOrganization
	}
	result, err := db.Exec(ctx, `UPDATE webhook_deliveries d SET status = $3, updated_at = CURRENT_TIMESTAMP
  WHERE organization_id = $1 AND id = $2 AND status = $4 AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)
  AND EXISTS (SELECT 1 FROM webhooks w WHERE w.organization_id = $1 AND w.id = d.webhook_id AND w.deleted_at IS NULL AND w.enabled)`,
		orgID, deliveryID, enums.DeliveryStatusDelivering, enums.DeliveryStatusPending)
	if err != nil {
		return false, errors.Wrap(err, "unable to mark webhook delivering")
	}
	return result.RowsAffected() > 0, nil
}

func CompleteDelivery(ctx context.Context, db database.Database, orgID, deliveryID int, status enums.DeliveryStatus) (bool, error) {
	if orgID <= 0 {
		return false, ErrInvalidOrganization
	}
	if !status.IsTerminal() {
		return false, ErrInvalidOptions
	}
	result, err := db.Exec(ctx, `UPDATE webhook_deliveries SET status = $3, updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP
  WHERE organization_id = $1 AND id = $2 AND status IN ($4,$5) AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, orgID, deliveryID, status, enums.DeliveryStatusPending, enums.DeliveryStatusDelivering)
	if err != nil {
		return false, errors.Wrap(err, "unable to complete webhook delivery")
	}
	return result.RowsAffected() > 0, nil
}

func scanDelivery(row database.Row) (*Delivery, error) {
	item := new(Delivery)
	err := row.Scan(&item.ID, &item.ExternalID, &item.OrganizationID, &item.WebhookID, &item.EventID, &item.WebhookExternalID, &item.EventExternalID,
		&item.Trigger, &item.ReplayedID, &item.ReplayedExternalID, &item.RequestKey, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to read webhook delivery")
	}
	return item, nil
}

func scanDeliveries(rows database.Rows) ([]*Delivery, error) {
	items := []*Delivery{}
	err := database.ScanRows(rows, func(row database.Row) error {
		item, err := scanDelivery(row)
		if err == nil {
			items = append(items, item)
		}
		return err
	})
	return items, err
}

// CancelUnavailable closes retained work after its organization or endpoint becomes unavailable.
func CancelUnavailable(ctx context.Context, db database.Database, orgID int, deliveryExternalID string) (bool, error) {
	if orgID <= 0 {
		return false, ErrInvalidOrganization
	}
	id, err := uuid.Parse(deliveryExternalID)
	if err != nil {
		return false, nil
	}
	result, err := db.Exec(ctx, `UPDATE webhook_deliveries d
  SET status = $3, updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP
  WHERE d.organization_id = $1 AND d.external_id = $2 AND d.status IN ($4,$5)
   AND (EXISTS (SELECT 1 FROM organizations o WHERE o.id = $1 AND o.deleted_at IS NOT NULL)
    OR EXISTS (SELECT 1 FROM webhooks w WHERE w.organization_id = $1 AND w.id = d.webhook_id
     AND (w.deleted_at IS NOT NULL OR NOT w.enabled)))`,
		orgID, id.String(), enums.DeliveryStatusCanceled, enums.DeliveryStatusPending, enums.DeliveryStatusDelivering)
	if err != nil {
		return false, errors.Wrap(err, "unable to cancel unavailable webhook delivery")
	}
	return result.RowsAffected() > 0, nil
}
