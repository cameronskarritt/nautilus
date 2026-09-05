package agentevents

import (
	"context"
	"database/sql"
	"encoding/json"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var ErrFenceViolation = errors.New("fence token mismatch: writer has been superseded")

type AppendOptions struct {
	Fence       int64
	Idempotency string
}

// Append appends an event to a stream with fence token validation.
// Returns ErrFenceViolation if the fence token doesn't match.
func Append(
	ctx context.Context,
	db database.Database,
	streamID int,
	eventType enums.AgentEventType,
	source enums.AgentEventSource,
	payload any,
	opts AppendOptions,
) (*Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal event payload")
	}

	var idempotencyKey any
	if opts.Idempotency != "" {
		idempotencyKey = opts.Idempotency
	}

	var id int
	err = database.Transact(ctx, db, func(tx database.Database) error {
		lockQuery := `
			UPDATE agent_streams
			SET fence_token = fence_token
			WHERE id = $1 AND fence_token = $2
			RETURNING id;
		`
		var lockedID int
		if err := tx.QueryRow(ctx, lockQuery, streamID, opts.Fence).Scan(&lockedID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrFenceViolation
			}
			return errors.Wrap(err, "unable to lock agent stream for event append")
		}

		query := `
			INSERT INTO agent_events(stream_id, sequence, type, source, payload, idempotency_key)
			VALUES (
				$1,
				COALESCE((SELECT MAX(sequence) FROM agent_events WHERE stream_id = $1), 0) + 1,
				$2, $3, $4, $5
			)
			ON CONFLICT (stream_id, type, idempotency_key)
				WHERE idempotency_key IS NOT NULL
				DO UPDATE SET idempotency_key = agent_events.idempotency_key
			RETURNING id;
		`
		if err := tx.QueryRow(ctx, query, streamID, eventType, source, payloadJSON, idempotencyKey).Scan(&id); err != nil {
			return errors.Wrap(err, "unable to append event to stream")
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrFenceViolation) {
			return nil, ErrFenceViolation
		}
		return nil, err
	}

	event, err := Get(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return event, nil
}

// Get retrieves an event by its internal ID.
func Get(ctx context.Context, db database.Database, id int) (*Event, error) {
	event := new(Event)

	query := `
		SELECT id, external_id, stream_id, sequence, type, source, idempotency_key, payload, created_at
		FROM agent_events
		WHERE id = $1;
	`

	var payloadJSON []byte
	err := db.QueryRow(ctx, query, id).Scan(
		&event.ID,
		&event.ExternalID,
		&event.StreamID,
		&event.Sequence,
		&event.Type,
		&event.Source,
		&event.IdempotencyKey,
		&payloadJSON,
		&event.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent event")
	}

	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &event.Payload); err != nil {
			return nil, errors.Wrap(err, "unable to unmarshal event payload")
		}
	}

	return event, nil
}

// ListByStream retrieves all events for a stream, optionally after a given sequence number.
func ListByStream(ctx context.Context, db database.Database, streamID int, afterSequence int64) ([]*Event, error) {
	query := `
		SELECT id, external_id, stream_id, sequence, type, source, idempotency_key, payload, created_at
		FROM agent_events
		WHERE stream_id = $1 AND sequence > $2
		ORDER BY sequence ASC;
	`

	rows, err := db.Query(ctx, query, streamID, afterSequence)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list agent events")
	}

	var events []*Event
	err = database.ScanRows(rows, func(row database.Row) error {
		e := new(Event)
		var payloadJSON []byte
		if err := row.Scan(
			&e.ID,
			&e.ExternalID,
			&e.StreamID,
			&e.Sequence,
			&e.Type,
			&e.Source,
			&e.IdempotencyKey,
			&payloadJSON,
			&e.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan agent event")
		}

		if len(payloadJSON) > 0 {
			if err := json.Unmarshal(payloadJSON, &e.Payload); err != nil {
				return errors.Wrap(err, "unable to unmarshal event payload")
			}
		}

		events = append(events, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return events, nil
}

// ListByType retrieves all events of a given type.
func ListByType(ctx context.Context, db database.Database, eventType enums.AgentEventType) ([]*Event, error) {
	query := `
		SELECT id, external_id, stream_id, sequence, type, source, idempotency_key, payload, created_at
		FROM agent_events
		WHERE type = $1
		ORDER BY created_at ASC;
	`

	rows, err := db.Query(ctx, query, eventType)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list agent events by type")
	}

	var events []*Event
	err = database.ScanRows(rows, func(row database.Row) error {
		e := new(Event)
		var payloadJSON []byte
		if err := row.Scan(
			&e.ID,
			&e.ExternalID,
			&e.StreamID,
			&e.Sequence,
			&e.Type,
			&e.Source,
			&e.IdempotencyKey,
			&payloadJSON,
			&e.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan agent event")
		}

		if len(payloadJSON) > 0 {
			if err := json.Unmarshal(payloadJSON, &e.Payload); err != nil {
				return errors.Wrap(err, "unable to unmarshal event payload")
			}
		}

		events = append(events, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return events, nil
}
