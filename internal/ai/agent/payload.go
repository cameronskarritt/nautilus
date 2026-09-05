package agent

import (
	"nautilus/internal/ai/llm"
	"nautilus/internal/enums"
)

type ApprovalResolvedPayload struct {
	ApprovalID string         `json:"approval_id"`
	Approved   bool           `json:"approved"`
	Reason     string         `json:"reason,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls"`
	Approver   Approver       `json:"approver"`
}

type TokenEventPayload struct {
	Type    enums.TokenType `json:"type"`
	Content string          `json:"content,omitempty"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Index   *int            `json:"index,omitempty"`
}

type ApprovalRequestedEventPayload struct {
	Type       string         `json:"type"`
	ApprovalID string         `json:"approval_id"`
	ToolCalls  []llm.ToolCall `json:"tool_calls"`
}

type ApprovalResolvedEventPayload struct {
	Type       string         `json:"type"`
	ApprovalID string         `json:"approval_id"`
	Approved   bool           `json:"approved"`
	Reason     string         `json:"reason,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls"`
	Approver   Approver       `json:"approver"`
}

type ToolResultEventPayload struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ErrorEventPayload struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}
