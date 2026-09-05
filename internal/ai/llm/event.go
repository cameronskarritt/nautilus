package llm

type Event struct {
	Type  EventType `json:"type"`
	Data  any       `json:"data,omitempty"`
	Error string    `json:"error,omitempty"`
}

type EventType string

const (
	EventTypeToolCall EventType = "tool_call"
	EventTypeError    EventType = "error"
	EventTypeMessage  EventType = "message"
	EventTypeThinking EventType = "thinking"
)

type EventLogger interface {
	Log(event *Event) error
}
