package agent

import (
	"context"
	"encoding/json"
	"time"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

// NoopEventLog implements eventlog.EventLog without persisting events.
type NoopEventLog struct{}

// NewNoopEventLog creates a NoopEventLog that discards all events.
func NewNoopEventLog() *NoopEventLog {
	return &NoopEventLog{}
}

// Append implements eventlog.EventLog.
func (l *NoopEventLog) Append(_ context.Context, streamID string, eventType enums.AgentEventType, source enums.AgentEventSource, payload any, tokens eventlog.Tokens) (*eventlog.Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal noop event payload")
	}
	return &eventlog.Event{
		ID:       "noop",
		StreamID: streamID,
		Sequence: 0,
		Type:     eventType,
		Source:   source,
		IdempotencyKey: func() *string {
			if tokens.Idempotency == "" {
				return nil
			}
			return &tokens.Idempotency
		}(),
		Payload:   payloadJSON,
		CreatedAt: time.Now(),
	}, nil
}

// List implements eventlog.EventLog.
func (l *NoopEventLog) List(_ context.Context, _ string, _ int64) ([]*eventlog.Event, error) {
	return []*eventlog.Event{}, nil
}

// Ensure NoopEventLog implements eventlog.EventLog at compile time.
var _ eventlog.EventLog = (*NoopEventLog)(nil)
