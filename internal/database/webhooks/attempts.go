package webhooks

import (
	"context"
	"database/sql"
	"strings"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
	"nautilus/internal/webhook"
)

func StartAttempt(ctx context.Context, db database.Database, orgID, deliveryID int, key, url string) (*Attempt, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	if deliveryID <= 0 || strings.TrimSpace(key) == "" || len(key) > 255 {
		return nil, ErrInvalidOptions
	}
	if err := webhook.ValidateURL(url); err != nil {
		return nil, err
	}
	var result *Attempt
	err := database.Transact(ctx, db, func(tx database.Database) error {
		var status enums.DeliveryStatus
		err := tx.QueryRow(ctx, `SELECT d.status FROM webhook_deliveries d JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
   WHERE d.organization_id = $1 AND d.id = $2 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) FOR UPDATE OF d`, orgID, deliveryID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "unable to lock webhook delivery")
		}
		result, err = scanAttempt(tx.QueryRow(ctx, `SELECT a.id, a.external_id, a.organization_id, a.delivery_id, a.attempt_key, a.url, a.started_at, a.finished_at, a.http_status, a.error_code FROM webhook_attempts a
 JOIN webhook_deliveries d ON d.organization_id = a.organization_id AND d.id = a.delivery_id
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 WHERE a.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND a.delivery_id = $2 AND a.attempt_key = $3`, orgID, deliveryID, key))
		if err != nil {
			return err
		}
		if result != nil {
			if status.IsTerminal() && !result.FinishedAt.Set {
				result = nil
				return nil
			}
			if result.URL != url {
				return ErrConflict
			}
			return nil
		}
		if status.IsTerminal() {
			return nil
		}
		result, err = scanAttempt(tx.QueryRow(ctx, `INSERT INTO webhook_attempts AS a(organization_id, delivery_id, attempt_key, url)
   SELECT $1,$2,$3,$4 FROM webhook_deliveries d JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
   WHERE d.organization_id = $1 AND d.id = $2 AND w.enabled AND w.deleted_at IS NULL
   RETURNING a.id, a.external_id, a.organization_id, a.delivery_id, a.attempt_key, a.url, a.started_at, a.finished_at, a.http_status, a.error_code`, orgID, deliveryID, key, url))
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func FinishAttempt(ctx context.Context, db database.Database, orgID, deliveryID, attemptID, httpStatus int, code enums.WebhookErrorCode) (*Attempt, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	if (httpStatus == 0) == (code == "") {
		return nil, ErrInvalidOptions
	}
	if httpStatus != 0 && (httpStatus < 100 || httpStatus > 599) || code != "" && !code.IsValid() {
		return nil, ErrInvalidOptions
	}
	status := optional.Empty[int]()
	if httpStatus != 0 {
		status = optional.Set(httpStatus)
	}
	errorCode := optional.Empty[string]()
	if code != "" {
		errorCode = optional.Set(string(code))
	}
	_, err := db.Exec(ctx, `UPDATE webhook_attempts a SET finished_at = CURRENT_TIMESTAMP, http_status = $4, error_code = $5
  WHERE organization_id = $1 AND delivery_id = $2 AND id = $3 AND finished_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)
  AND EXISTS (SELECT 1 FROM webhook_deliveries d JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
   WHERE d.organization_id = $1 AND d.id = a.delivery_id AND w.deleted_at IS NULL)`, orgID, deliveryID, attemptID, status, errorCode)
	if err != nil {
		return nil, errors.Wrap(err, "unable to finish webhook attempt")
	}
	return scanAttempt(db.QueryRow(ctx, `SELECT a.id, a.external_id, a.organization_id, a.delivery_id, a.attempt_key, a.url, a.started_at, a.finished_at, a.http_status, a.error_code FROM webhook_attempts a
 JOIN webhook_deliveries d ON d.organization_id = a.organization_id AND d.id = a.delivery_id
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 WHERE a.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND a.delivery_id = $2 AND a.id = $3`, orgID, deliveryID, attemptID))
}

func ListAttempts(ctx context.Context, db database.Database, orgID int, webhookExternalID, deliveryExternalID string, params pagination.Params) (pagination.Page[*Attempt], error) {
	if orgID <= 0 {
		return pagination.Page[*Attempt]{}, ErrInvalidOrganization
	}
	webhookID, err := uuid.Parse(webhookExternalID)
	if err != nil {
		return pagination.Page[*Attempt]{Data: []*Attempt{}}, nil
	}
	deliveryID, err := uuid.Parse(deliveryExternalID)
	if err != nil {
		return pagination.Page[*Attempt]{Data: []*Attempt{}}, nil
	}
	scope := "attempts:" + webhookID.String() + ":" + deliveryID.String()
	limit, before, err := pageParams(params, orgID, scope)
	if err != nil {
		return pagination.Page[*Attempt]{}, err
	}
	rows, err := db.Query(ctx, `SELECT a.id, a.external_id, a.organization_id, a.delivery_id, a.attempt_key, a.url, a.started_at, a.finished_at, a.http_status, a.error_code FROM webhook_attempts a
 JOIN webhook_deliveries d ON d.organization_id = a.organization_id AND d.id = a.delivery_id
 JOIN webhooks w ON w.organization_id = d.organization_id AND w.id = d.webhook_id
 WHERE a.organization_id = $1 AND w.deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) AND w.external_id = $2 AND d.external_id = $3
  AND ($5::bigint = 0 OR a.id < $5) ORDER BY a.id DESC LIMIT $4`, orgID, webhookID.String(), deliveryID.String(), limit+1, before)
	if err != nil {
		return pagination.Page[*Attempt]{}, errors.Wrap(err, "unable to list webhook attempts")
	}
	items := []*Attempt{}
	err = database.ScanRows(rows, func(row database.Row) error {
		item, err := scanAttempt(row)
		if err == nil {
			items = append(items, item)
		}
		return err
	})
	if err != nil {
		return pagination.Page[*Attempt]{}, err
	}
	return pagination.Build(items, limit, func(item *Attempt) pagination.Cursor { return pageCursor(item.ID, orgID, scope) }), nil
}

func scanAttempt(row database.Row) (*Attempt, error) {
	item := new(Attempt)
	var code optional.Optional[string]
	err := row.Scan(&item.ID, &item.ExternalID, &item.OrganizationID, &item.DeliveryID, &item.AttemptKey, &item.URL, &item.StartedAt, &item.FinishedAt, &item.HTTPStatus, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to read webhook attempt")
	}
	if code.Set {
		item.ErrorCode = optional.Set(enums.WebhookErrorCode(code.Data))
	}
	return item, nil
}
