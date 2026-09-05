package enums

type Queue string

const (
	// QueueAgentSignals is the queue for agent signals (new messages, stop commands, etc.)
	QueueAgentSignals Queue = "agent-signals"
)
