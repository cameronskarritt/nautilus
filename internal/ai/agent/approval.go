package agent

import "nautilus/internal/ai/llm"

// Approver identifies who approved/rejected a tool call batch.
type Approver struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

// ApprovalDecision is the payload for signal.approval events.
type ApprovalDecision struct {
	ApprovalID string   `json:"approval_id"`
	Approved   bool     `json:"approved"`
	Reason     string   `json:"reason,omitempty"`
	Approver   Approver `json:"approver"`
}

// PendingApproval tracks a suspended tool call batch awaiting user approval.
type PendingApproval struct {
	ApprovalID string         `json:"approval_id"`
	Calls      []llm.ToolCall `json:"tool_calls"`
}
