package llmtrace

import "context"

type LLMTracer interface {
	Start(ctx context.Context, call *Call) (context.Context, Span)
}

type Span interface {
	End(result *Result)
	RecordError(err error)
}

type Call struct {
	Operation string
	Provider  string
	Model     string
	Prompt    string
	Streaming bool
}

type Result struct {
	Completion   string
	Usage        *Usage
	FinishReason string
}

type Usage struct {
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	CachedInputTokens     int
	ReasoningOutputTokens int
}
