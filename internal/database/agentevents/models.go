package agentevents

import (
	"time"

	"nautilus/internal/enums"
)

type Event struct {
	ID             int                    `json:"-"`
	ExternalID     string                 `json:"id"`
	StreamID       int                    `json:"-"`
	Sequence       int64                  `json:"sequence"`
	Type           enums.AgentEventType   `json:"type"`
	Source         enums.AgentEventSource `json:"source"`
	IdempotencyKey *string                `json:"idempotency_key,omitempty"`
	Payload        any                    `json:"payload,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}
