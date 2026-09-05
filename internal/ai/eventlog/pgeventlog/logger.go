package pgeventlog

import (
	"context"
	"encoding/json"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/database"
	"nautilus/internal/database/agentevents"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

// Logger implements eventlog.EventLog using Postgres.
type Logger struct {
	db database.Database
}

// New creates a new Postgres-backed EventLog.
func New(db database.Database) *Logger {
	return &Logger{
		db: db,
	}
}

func convertEvent(streamID string, event *agentevents.Event) (*eventlog.Event, error) {
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal payload")
	}

	return &eventlog.Event{
		ID:             event.ExternalID,
		StreamID:       streamID,
		Sequence:       event.Sequence,
		Type:           event.Type,
		Source:         event.Source,
		IdempotencyKey: event.IdempotencyKey,
		Payload:        payloadJSON,
		CreatedAt:      event.CreatedAt,
	}, nil
}

// Append adds an event to the log with fence token validation.
func (l *Logger) Append(ctx context.Context, streamID string, eventType enums.AgentEventType, source enums.AgentEventSource, payload any, tokens eventlog.Tokens) (*eventlog.Event, error) {
	// Get stream by external ID
	stream, err := agentstreams.GetByExternalID(ctx, l.db, streamID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get stream")
	}
	if stream == nil {
		return nil, errors.Errorf("stream not found: %s", streamID)
	}

	event, err := agentevents.Append(ctx, l.db, stream.ID, eventType, source, payload, agentevents.AppendOptions{
		Fence:       tokens.Fence,
		Idempotency: tokens.Idempotency,
	})
	if err != nil {
		if errors.Is(err, agentevents.ErrFenceViolation) {
			return nil, eventlog.ErrFenceViolation
		}
		return nil, err
	}

	return convertEvent(streamID, event)
}

// List returns events for a stream after a given sequence number.
func (l *Logger) List(ctx context.Context, streamID string, afterSequence int64) ([]*eventlog.Event, error) {
	// Get stream by external ID
	stream, err := agentstreams.GetByExternalID(ctx, l.db, streamID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get stream")
	}
	if stream == nil {
		return nil, errors.Errorf("stream not found: %s", streamID)
	}

	// List events
	events, err := agentevents.ListByStream(ctx, l.db, stream.ID, afterSequence)
	if err != nil {
		return nil, err
	}

	// Convert to eventlog.Event
	result := make([]*eventlog.Event, 0, len(events))
	for _, event := range events {
		converted, err := convertEvent(streamID, event)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}

	return result, nil
}
