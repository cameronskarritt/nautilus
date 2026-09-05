package eventlog

import (
	"context"
	"encoding/json"
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var ErrFenceViolation = errors.New("fence token mismatch: writer has been superseded")

type Tokens struct {
	Fence       int64
	Idempotency string
}

// EventLog provides append-only event logging with fence token validation.
type EventLog interface {
	// Append adds an event to the log for the given stream.
	// tokens.Fence must match the stream's current fence_token or ErrFenceViolation is returned.
	Append(ctx context.Context, streamID string, eventType enums.AgentEventType, source enums.AgentEventSource, payload any, tokens Tokens) (*Event, error)

	// List returns events for a stream, optionally starting after a sequence number.
	List(ctx context.Context, streamID string, afterSequence int64) ([]*Event, error)
}

// Event represents a single event in the log.
type Event struct {
	ID             string                 `json:"id"`
	StreamID       string                 `json:"stream_id"`
	Sequence       int64                  `json:"sequence"`
	Type           enums.AgentEventType   `json:"type"`
	Source         enums.AgentEventSource `json:"source"`
	IdempotencyKey *string                `json:"idempotency_key,omitempty"`
	Payload        json.RawMessage        `json:"payload,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}
