package agentapprovals

import (
	"time"

	"nautilus/internal/ai/llm"
	"nautilus/internal/optional"
)

type Approval struct {
	ID              int                          `json:"-"`
	ExternalID      string                       `json:"id"`
	StreamID        int                          `json:"-"`
	Status          string                       `json:"status"`
	ToolCalls       []llm.ToolCall               `json:"tool_calls"`
	Reason          optional.Optional[string]    `json:"reason,omitzero"`
	ApprovedBy      optional.Optional[int]       `json:"-"`
	ApproverMessage optional.Optional[string]    `json:"approver_message,omitzero"`
	RequestedAt     time.Time                    `json:"requested_at"`
	ResolvedAt      optional.Optional[time.Time] `json:"resolved_at,omitzero"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
}

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)
