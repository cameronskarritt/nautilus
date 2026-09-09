package webhooks

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
	"nautilus/internal/querybuilder"
	"nautilus/internal/webhook"
)

const Retention = 30 * 24 * time.Hour
const MaxEndpoints = 10

var (
	ErrInvalidOrganization = errors.New("invalid webhook organization")
	ErrInvalidOptions      = errors.New("invalid webhook options")
	ErrInvalidName         = errors.New("invalid webhook name")
	ErrInvalidEventTypes   = errors.New("invalid webhook event types")
	ErrInvalidSecret       = errors.New("invalid webhook signing secret")
	ErrInvalidCursor       = errors.New("invalid webhook cursor")
	ErrLimit               = errors.New("webhook endpoint limit reached")
	ErrConflict            = errors.New("webhook idempotency conflict")
)

func Create(ctx context.Context, db database.Database, orgID int, opts *CreateOptions) (*Webhook, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	if opts == nil {
		return nil, ErrInvalidOptions
	}
	id, err := uuid.Parse(opts.ExternalID)
	if err != nil {
		return nil, ErrInvalidOptions
	}
	name := strings.TrimSpace(opts.Name)
	if !webhook.ValidName(name) {
		return nil, ErrInvalidName
	}
	if err := webhook.ValidateURL(opts.URL); err != nil {
		return nil, err
	}
	types, err := normalizeTypes(opts.EventTypes)
	if err != nil {
		return nil, err
	}
	if len(opts.SigningSecret) == 0 {
		return nil, ErrInvalidSecret
	}
	var result *Webhook
	err = database.Transact(ctx, db, func(tx database.Database) error {
		var owner int
		err := tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, orgID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "unable to lock webhook organization")
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM webhooks WHERE organization_id = $1 AND deleted_at IS NULL`, orgID).Scan(&count); err != nil {
			return errors.Wrap(err, "unable to count webhooks")
		}
		if count >= MaxEndpoints {
			return ErrLimit
		}
		result, err = scan(tx.QueryRow(ctx, `INSERT INTO webhooks(external_id, organization_id, name, url, event_types, signing_secret)
   VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at`, id.String(), orgID, name, opts.URL, types, opts.SigningSecret))
		return err
	})
	return result, err
}

func GetByExternalID(ctx context.Context, db database.Database, orgID int, externalID string) (*Webhook, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	return scan(db.QueryRow(ctx, `SELECT id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at FROM webhooks WHERE organization_id = $1 AND external_id = $2 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, orgID, id.String()))
}

func Get(ctx context.Context, db database.Database, orgID, id int) (*Webhook, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	return scan(db.QueryRow(ctx, `SELECT id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at FROM webhooks WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, orgID, id))
}

func Update(ctx context.Context, db database.Database, orgID int, externalID string, opts *UpdateOptions) (*Webhook, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	if opts == nil || (!opts.Name.Set && !opts.URL.Set && !opts.EventTypes.Set && !opts.Enabled.Set) {
		return nil, ErrInvalidOptions
	}
	values := *opts
	if values.Name.Set {
		values.Name.Data = strings.TrimSpace(values.Name.Data)
		if !webhook.ValidName(values.Name.Data) {
			return nil, ErrInvalidName
		}
	}
	if values.URL.Set {
		if err := webhook.ValidateURL(values.URL.Data); err != nil {
			return nil, err
		}
	}
	types := optional.Empty[[]string]()
	if values.EventTypes.Set {
		normalized, err := normalizeTypes(values.EventTypes.Data)
		if err != nil {
			return nil, err
		}
		types = optional.Set(normalized)
	}
	var result *Webhook
	err = database.Transact(ctx, db, func(tx database.Database) error {
		current, err := scan(tx.QueryRow(ctx, `SELECT id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at FROM webhooks WHERE organization_id = $1 AND external_id = $2 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) FOR UPDATE`, orgID, id.String()))
		if err != nil || current == nil {
			return err
		}
		query, args, err := querybuilder.Update("webhooks").Set("name", values.Name, "url", values.URL, "event_types", types,
			"enabled", values.Enabled, "updated_at", querybuilder.Expr("CURRENT_TIMESTAMP")).Where(querybuilder.Eq{"organization_id": orgID, "id": current.ID}).Returning(`id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at`).Build()
		if err != nil {
			return errors.Wrap(err, "unable to build webhook update")
		}
		result, err = scan(tx.QueryRow(ctx, query, args...))
		if err != nil {
			return err
		}
		if values.Enabled.Set && !values.Enabled.Data {
			return cancelDeliveries(ctx, tx, orgID, current.ID)
		}
		return nil
	})
	return result, err
}

func Delete(ctx context.Context, db database.Database, orgID int, externalID string) (bool, error) {
	if orgID <= 0 {
		return false, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return false, nil
	}
	var removed bool
	err = database.Transact(ctx, db, func(tx database.Database) error {
		var webhookID int
		err := tx.QueryRow(ctx, `UPDATE webhooks SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
   WHERE organization_id = $1 AND external_id = $2 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) RETURNING id`, orgID, id.String()).Scan(&webhookID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "unable to delete webhook")
		}
		removed = true
		return cancelDeliveries(ctx, tx, orgID, webhookID)
	})
	return removed, err
}

func RotateSecret(ctx context.Context, db database.Database, orgID int, externalID string, secret []byte, expiresAt time.Time) (*Webhook, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	if len(secret) == 0 || !expiresAt.After(time.Now()) {
		return nil, ErrInvalidSecret
	}
	var result *Webhook
	err = database.Transact(ctx, db, func(tx database.Database) error {
		current, err := scan(tx.QueryRow(ctx, `SELECT id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at FROM webhooks WHERE organization_id = $1 AND external_id = $2 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL) FOR UPDATE`, orgID, id.String()))
		if err != nil || current == nil {
			return err
		}
		if current.PreviousSecretExpiresAt.Set && current.PreviousSecretExpiresAt.Data.After(time.Now()) {
			return ErrConflict
		}
		result, err = scan(tx.QueryRow(ctx, `UPDATE webhooks SET previous_signing_secret = signing_secret, signing_secret = $3,
			previous_secret_expires_at = $4, updated_at = CURRENT_TIMESTAMP
			WHERE organization_id = $1 AND id = $2 RETURNING id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at`, orgID, current.ID, secret, expiresAt))
		return err
	})
	return result, err
}

func cancelDeliveries(ctx context.Context, db database.Database, orgID, webhookID int) error {
	_, err := db.Exec(ctx, `UPDATE webhook_deliveries SET status = $3, completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
  WHERE organization_id = $1 AND webhook_id = $2 AND status IN ($4,$5)`, orgID, webhookID, enums.DeliveryStatusCanceled, enums.DeliveryStatusPending, enums.DeliveryStatusDelivering)
	return errors.Wrap(err, "unable to cancel webhook deliveries")
}

func List(ctx context.Context, db database.Database, orgID int, params pagination.Params) (pagination.Page[*Webhook], error) {
	if orgID <= 0 {
		return pagination.Page[*Webhook]{}, ErrInvalidOrganization
	}
	scope := "webhooks"
	limit, before, err := pageParams(params, orgID, scope)
	if err != nil {
		return pagination.Page[*Webhook]{}, err
	}
	rows, err := db.Query(ctx, `SELECT id, external_id, organization_id, name, url, event_types, enabled, signing_secret,
 previous_signing_secret, previous_secret_expires_at, created_at, updated_at, deleted_at FROM webhooks WHERE organization_id = $1 AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)
  AND ($3::bigint = 0 OR id < $3) ORDER BY id DESC LIMIT $2`, orgID, limit+1, before)
	if err != nil {
		return pagination.Page[*Webhook]{}, errors.Wrap(err, "unable to list webhooks")
	}
	items := make([]*Webhook, 0, limit+1)
	err = database.ScanRows(rows, func(row database.Row) error {
		item, err := scan(row)
		if err == nil {
			items = append(items, item)
		}
		return err
	})
	if err != nil {
		return pagination.Page[*Webhook]{}, err
	}
	return pagination.Build(items, limit, func(item *Webhook) pagination.Cursor { return pageCursor(item.ID, orgID, scope) }), nil
}

func normalizeTypes(types []enums.WebhookEventType) ([]string, error) {
	if len(types) == 0 {
		return nil, ErrInvalidEventTypes
	}
	result := make([]string, 0, len(types))
	for _, t := range types {
		if !t.IsValid() {
			return nil, ErrInvalidEventTypes
		}
		result = append(result, string(t))
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func scan(row database.Row) (*Webhook, error) {
	result := new(Webhook)
	var types []string
	err := row.Scan(&result.ID, &result.ExternalID, &result.OrganizationID, &result.Name, &result.URL, &types, &result.Enabled,
		&result.SigningSecret, &result.PreviousSigningSecret, &result.PreviousSecretExpiresAt, &result.CreatedAt, &result.UpdatedAt, &result.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to read webhook")
	}
	result.EventTypes = make([]enums.WebhookEventType, len(types))
	for i, t := range types {
		result.EventTypes[i] = enums.WebhookEventType(t)
	}
	return result, nil
}
