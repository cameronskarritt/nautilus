package outboxevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

var ErrLeaseLost = errors.New("outbox lease lost")

func Enqueue(
	ctx context.Context,
	db database.Database,
	organizationID int,
	topic, aggregateID, idempotencyKey string,
	payload any,
	availableAt time.Time,
) (*Event, error) {
	if idempotencyKey == "" {
		return nil, errors.New("outbox idempotency key is required")
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal outbox payload")
	}
	var available any
	if !availableAt.IsZero() {
		available = availableAt
	}
	query := `
		INSERT INTO outbox_events(
			organization_id, topic, aggregate_id, idempotency_key, payload, available_at
		)
		VALUES ($1, $2, $3, $4, $5, COALESCE($6, CURRENT_TIMESTAMP))
		ON CONFLICT (organization_id, idempotency_key)
		DO UPDATE SET idempotency_key = outbox_events.idempotency_key
		RETURNING id;
	`
	var id int
	err = db.QueryRow(
		ctx,
		query,
		organizationID,
		topic,
		aggregateID,
		idempotencyKey,
		payloadJSON,
		available,
	).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to enqueue outbox event")
	}
	return Get(ctx, db, organizationID, id)
}

func Claim(
	ctx context.Context,
	db database.Database,
	topic string,
	lease time.Duration,
) (*Event, error) {
	if topic == "" {
		return nil, errors.New("outbox topic is required")
	}
	if lease <= 0 {
		return nil, errors.New("outbox lease must be positive")
	}
	var event *Event
	err := database.Transact(ctx, db, func(tx database.Database) error {
		query := `
			WITH candidate AS (
				SELECT id
				FROM outbox_events
				WHERE topic = $1 AND processed_at IS NULL
					AND available_at <= CURRENT_TIMESTAMP
					AND (lease_expires_at IS NULL OR lease_expires_at <= CURRENT_TIMESTAMP)
				ORDER BY available_at, id
				FOR UPDATE SKIP LOCKED
				LIMIT 1
			)
			UPDATE outbox_events e
			SET attempts = attempts + 1,
				lease_token = uuid_generate_v4(),
				lease_expires_at = CURRENT_TIMESTAMP + ($2 * INTERVAL '1 second'),
				updated_at = CURRENT_TIMESTAMP
			FROM candidate
			WHERE e.id = candidate.id
			RETURNING e.id, e.external_id, e.organization_id, e.topic, e.aggregate_id,
				e.idempotency_key, e.payload, e.available_at, e.processed_at,
				e.attempts, e.lease_token, e.lease_expires_at, e.created_at;
		`
		claimed, err := scanEvent(tx.QueryRow(ctx, query, topic, lease.Seconds()))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return errors.Wrap(err, "unable to claim outbox event")
		}
		event = claimed
		return nil
	})
	return event, err
}

func MarkProcessed(
	ctx context.Context,
	db database.Database,
	organizationID, id int,
	leaseToken string,
) error {
	if leaseToken == "" {
		return errors.New("outbox lease token is required")
	}
	query := `
		UPDATE outbox_events
		SET processed_at = CURRENT_TIMESTAMP, lease_token = NULL,
			lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE organization_id = $1 AND id = $2 AND processed_at IS NULL
			AND lease_token = $3 AND lease_expires_at > CURRENT_TIMESTAMP;
	`
	result, err := db.Exec(ctx, query, organizationID, id, leaseToken)
	if err != nil {
		return errors.Wrap(err, "unable to mark outbox event processed")
	}
	if result.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func Get(ctx context.Context, db database.Database, organizationID, id int) (*Event, error) {
	query := `
		SELECT id, external_id, organization_id, topic, aggregate_id, idempotency_key,
			payload, available_at, processed_at, attempts, lease_token,
			lease_expires_at, created_at
		FROM outbox_events
		WHERE organization_id = $1 AND id = $2;
	`
	event, err := scanEvent(db.QueryRow(ctx, query, organizationID, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch outbox event")
	}
	return event, nil
}

func scanEvent(row database.Row) (*Event, error) {
	event := new(Event)
	err := row.Scan(
		&event.ID,
		&event.ExternalID,
		&event.OrganizationID,
		&event.Topic,
		&event.AggregateID,
		&event.IdempotencyKey,
		&event.Payload,
		&event.AvailableAt,
		&event.ProcessedAt,
		&event.Attempts,
		&event.LeaseToken,
		&event.LeaseExpiresAt,
		&event.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return event, nil
}
