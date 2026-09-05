package agentstreams

import (
	"time"
)

type Stream struct {
	ID         int       `json:"-"`
	ExternalID string    `json:"id"`
	UserID     int       `json:"-"`
	OrgID      int       `json:"-"`
	Status     Status    `json:"status"`
	FenceToken int64     `json:"-"`
	Title      *string   `json:"title"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Status string

const (
	StatusPending          Status = "pending"
	StatusRunning          Status = "running"
	StatusIdle             Status = "idle"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
	StatusAwaitingApproval Status = "awaiting_approval"
)
