// Package webhookevents records immutable, organization-owned business events.
package webhookevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var (
	ErrInvalidEvent = errors.New("invalid event")
	ErrConflict     = errors.New("event key already identifies another occurrence")
)

type Event struct {
	ID             int                    `json:"-"`
	ExternalID     string                 `json:"id"`
	OrganizationID int                    `json:"-"`
	Type           enums.WebhookEventType `json:"type"`
	SchemaVersion  int                    `json:"schema_version"`
	IdempotencyKey string                 `json:"-"`
	Payload        json.RawMessage        `json:"payload"`
	OccurredAt     time.Time              `json:"occurred_at"`
	CreatedAt      time.Time              `json:"created_at"`
}

type CreateOptions struct {
	Type           enums.WebhookEventType
	IdempotencyKey string
	OccurredAt     time.Time
	DocumentID     string
}

type Payload struct {
	ID             string                 `json:"id"`
	Type           enums.WebhookEventType `json:"type"`
	SchemaVersion  int                    `json:"schema_version"`
	OccurredAt     time.Time              `json:"occurred_at"`
	OrganizationID string                 `json:"organization_id"`
	Data           Data                   `json:"data"`
}

type Data struct {
	DocumentID string `json:"document_id,omitempty"`
}

// Record returns the original event on retries. The boolean is true only when
// this call inserted it, so callers can freeze recipients in the same transaction.
func Record(ctx context.Context, db database.Database, orgID int, opts *CreateOptions) (*Event, bool, error) {
	if orgID <= 0 || opts == nil || !opts.Type.IsValid() || opts.OccurredAt.IsZero() || len(opts.IdempotencyKey) == 0 || len(opts.IdempotencyKey) > 255 || strings.ContainsFunc(opts.IdempotencyKey, unicode.IsControl) {
		return nil, false, ErrInvalidEvent
	}
	documentID := ""
	var documentUUID any
	if opts.Type == enums.WebhookEventTypeDocumentAvailable {
		id, err := uuid.Parse(opts.DocumentID)
		if err != nil {
			return nil, false, ErrInvalidEvent
		}
		documentID = id.String()
		documentUUID = documentID
	} else if opts.DocumentID != "" {
		return nil, false, ErrInvalidEvent
	}

	var orgExternalID string
	err := db.QueryRow(ctx, `SELECT external_id FROM organizations WHERE id = $1 AND deleted_at IS NULL`, orgID).Scan(&orgExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "unable to read event organization")
	}
	payload := Payload{
		ID: uuid.NewV4().String(), Type: opts.Type, SchemaVersion: 1,
		OccurredAt: opts.OccurredAt.UTC().Truncate(time.Microsecond), OrganizationID: orgExternalID,
		Data: Data{DocumentID: documentID},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false, errors.Wrap(err, "unable to encode event")
	}
	event, err := scan(db.QueryRow(ctx, `INSERT INTO webhook_events
		(external_id, organization_id, type, schema_version, idempotency_key, payload, occurred_at)
		SELECT $1, id, $3, $4, $5, $6, $7 FROM organizations WHERE id = $2 AND deleted_at IS NULL
		AND ($8::uuid IS NULL OR EXISTS (SELECT 1 FROM documents
			WHERE organization_id = $2 AND external_id = $8 AND status = $9))
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
		RETURNING id, external_id, organization_id, type, schema_version, idempotency_key, payload, occurred_at, created_at`,
		payload.ID, orgID, opts.Type, payload.SchemaVersion, opts.IdempotencyKey, body, payload.OccurredAt,
		documentUUID, enums.DocumentStatusUploaded))
	if err != nil || event != nil {
		return event, event != nil, err
	}
	event, err = GetByKey(ctx, db, orgID, opts.IdempotencyKey)
	if err != nil || event == nil {
		return event, false, err
	}
	var existing Payload
	if err := json.Unmarshal(event.Payload, &existing); err != nil {
		return nil, false, errors.Wrap(err, "unable to decode stored event")
	}
	if event.Type != opts.Type || existing.Data.DocumentID != documentID {
		return nil, false, ErrConflict
	}
	return event, false, nil
}

func GetByExternalID(ctx context.Context, db database.Database, orgID int, externalID string) (*Event, error) {
	if orgID <= 0 {
		return nil, ErrInvalidEvent
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	return scan(db.QueryRow(ctx, `SELECT id, external_id, organization_id, type, schema_version,
		idempotency_key, payload, occurred_at, created_at FROM webhook_events WHERE organization_id = $1 AND external_id = $2
		AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, orgID, id.String()))
}

func GetByKey(ctx context.Context, db database.Database, orgID int, key string) (*Event, error) {
	if orgID <= 0 {
		return nil, ErrInvalidEvent
	}
	return scan(db.QueryRow(ctx, `SELECT id, external_id, organization_id, type, schema_version,
		idempotency_key, payload, occurred_at, created_at FROM webhook_events WHERE organization_id = $1 AND idempotency_key = $2
		AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, orgID, key))
}

func scan(row database.Row) (*Event, error) {
	event := new(Event)
	err := row.Scan(&event.ID, &event.ExternalID, &event.OrganizationID, &event.Type, &event.SchemaVersion,
		&event.IdempotencyKey, &event.Payload, &event.OccurredAt, &event.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to read event")
	}
	return event, nil
}
