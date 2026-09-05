package outboxevents

import (
	"encoding/json"
	"time"

	"nautilus/internal/optional"
)

type Event struct {
	ID             int                          `json:"-"`
	ExternalID     string                       `json:"id"`
	OrganizationID int                          `json:"-"`
	Topic          string                       `json:"topic"`
	AggregateID    string                       `json:"aggregate_id"`
	IdempotencyKey string                       `json:"idempotency_key"`
	Payload        json.RawMessage              `json:"payload"`
	AvailableAt    time.Time                    `json:"available_at"`
	ProcessedAt    optional.Optional[time.Time] `json:"processed_at,omitzero"`
	Attempts       int                          `json:"attempts"`
	LeaseToken     optional.Optional[string]    `json:"-"`
	LeaseExpiresAt optional.Optional[time.Time] `json:"-"`
	CreatedAt      time.Time                    `json:"created_at"`
}
