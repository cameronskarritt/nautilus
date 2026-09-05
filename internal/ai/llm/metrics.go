package llm

import (
	"time"

	"nautilus/internal/enums"
)

// Metrics holds timing and token usage data for LLM requests.
type Metrics struct {
	StartTime        time.Time `json:"start_time"`
	FirstTokenTime   time.Time `json:"first_token_time"`   // Any token (including reasoning)
	FirstContentTime time.Time `json:"first_content_time"` // First text/tool_call token (excludes reasoning)
	EndTime          time.Time `json:"end_time"`

	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// TTFT returns the time to first token (any token including reasoning).
func (m *Metrics) TTFT() time.Duration {
	if m.FirstTokenTime.IsZero() {
		return 0
	}
	return m.FirstTokenTime.Sub(m.StartTime)
}

// TotalDuration returns the total request duration.
func (m *Metrics) TotalDuration() time.Duration {
	if m.EndTime.IsZero() {
		return 0
	}
	return m.EndTime.Sub(m.StartTime)
}

// ThinkingDuration returns the time spent in the reasoning/thinking phase.
// This is the duration between the first token (reasoning) and first content token.
// Returns 0 if there was no reasoning phase.
func (m *Metrics) ThinkingDuration() time.Duration {
	if m.FirstTokenTime.IsZero() || m.FirstContentTime.IsZero() {
		return 0
	}
	return m.FirstContentTime.Sub(m.FirstTokenTime)
}

// TokensPerSecond returns the output tokens per second.
func (m *Metrics) TokensPerSecond() float64 {
	duration := m.TotalDuration()
	if duration == 0 || m.OutputTokens == 0 {
		return 0
	}
	return float64(m.OutputTokens) / duration.Seconds()
}

// Metricer provides access to request metrics.
// Use type assertion to access metrics from a TokenStream:
//
//	if m, ok := stream.(llm.Metricer); ok {
//	    metrics := m.Metrics()
//	}
type Metricer interface {
	Metrics() *Metrics
}

// metricsTokenStream wraps a TokenStream to capture timing metrics.
type metricsTokenStream struct {
	inner   TokenStream
	metrics *Metrics
	tokens  chan Token
	done    chan struct{}
}

// WithMetrics wraps a TokenStream to capture timing metrics.
// The metrics are populated as tokens flow through the stream.
func WithMetrics(stream TokenStream) TokenStream {
	m := &metricsTokenStream{
		inner: stream,
		metrics: &Metrics{
			StartTime: time.Now(),
		},
		tokens: make(chan Token),
		done:   make(chan struct{}),
	}

	go m.process()

	return m
}

func (m *metricsTokenStream) process() {
	defer close(m.tokens)
	defer close(m.done)

	firstToken := true
	firstContent := true

	for token := range m.inner.Tokens() {
		now := time.Now()

		// Record first token time (any token)
		if firstToken {
			m.metrics.FirstTokenTime = now
			firstToken = false
		}

		// Record first content token time (text or tool_call, not reasoning)
		tokenType := token.TokenType()
		if firstContent && (tokenType == enums.TokenTypeText || tokenType == enums.TokenTypeToolCall) {
			m.metrics.FirstContentTime = now
			firstContent = false
		}

		// Capture usage data
		if tokenType == enums.TokenTypeUsage {
			if ut, ok := token.(*UsageToken); ok {
				m.metrics.InputTokens = ut.InputTokens
				m.metrics.OutputTokens = ut.OutputTokens
			}
		}

		m.tokens <- token
	}

	m.metrics.EndTime = time.Now()
}

func (m *metricsTokenStream) Tokens() <-chan Token {
	return m.tokens
}

func (m *metricsTokenStream) Cancel() {
	m.inner.Cancel()
}

func (m *metricsTokenStream) Metrics() *Metrics {
	// Wait for processing to complete before returning metrics
	<-m.done
	return m.metrics
}
