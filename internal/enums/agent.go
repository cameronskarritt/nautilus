package enums

type AgentEventType string

const (
	// Signal events
	AgentEventSignalReceived AgentEventType = "signal.received"
	AgentEventSignalStop     AgentEventType = "signal.stop"

	// Turn lifecycle
	AgentEventTurnStarted   AgentEventType = "turn.started"
	AgentEventTurnCompleted AgentEventType = "turn.completed"

	// LLM events
	AgentEventLLMRequest   AgentEventType = "llm.request"
	AgentEventLLMResponse  AgentEventType = "llm.response"
	AgentEventLLMText      AgentEventType = "llm.text"
	AgentEventLLMReasoning AgentEventType = "llm.reasoning"

	// Tool events
	AgentEventToolCall   AgentEventType = "tool.call"
	AgentEventToolResult AgentEventType = "tool.result"
	AgentEventToolRepair AgentEventType = "tool.repair"

	// Approval events
	AgentEventSignalApproval    AgentEventType = "signal.approval"
	AgentEventApprovalRequested AgentEventType = "approval.requested"
	AgentEventApprovalResolved  AgentEventType = "approval.resolved"

	// Message events
	AgentEventMessage AgentEventType = "message"

	// Error events
	AgentEventError AgentEventType = "error"
)

func (t AgentEventType) String() string {
	return string(t)
}

type AgentEventSource string

const (
	AgentEventSourceAPI      AgentEventSource = "api"
	AgentEventSourceAgent    AgentEventSource = "agent"
	AgentEventSourceInternal AgentEventSource = "internal"
)

func (s AgentEventSource) String() string {
	return string(s)
}
