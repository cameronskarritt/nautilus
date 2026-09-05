package llm

import (
	"strings"

	"nautilus/internal/enums"
	"nautilus/internal/observability/llmtrace"
)

type tracedTokenStream struct {
	inner  TokenStream
	span   llmtrace.Span
	tokens chan Token
}

func TraceTokenStream(stream TokenStream, span llmtrace.Span) TokenStream {
	if stream == nil || span == nil {
		return stream
	}

	traced := &tracedTokenStream{
		inner:  stream,
		span:   span,
		tokens: make(chan Token),
	}
	go traced.process()
	return traced
}

func (s *tracedTokenStream) Tokens() <-chan Token {
	return s.tokens
}

func (s *tracedTokenStream) Cancel() {
	s.inner.Cancel()
}

func (s *tracedTokenStream) Metrics() *Metrics {
	metricer, ok := s.inner.(Metricer)
	if !ok {
		return nil
	}
	return metricer.Metrics()
}

func (s *tracedTokenStream) process() {
	var completion strings.Builder
	var usage *llmtrace.Usage
	var finishReason string
	var streamErr error

	for token := range s.inner.Tokens() {
		switch t := token.(type) {
		case *TextToken:
			if t.TokenType() == enums.TokenTypeText {
				completion.WriteString(t.Content())
			}
		case *ToolCallToken:
			completion.WriteString(t.Content())
		case *UsageToken:
			usage = &llmtrace.Usage{
				InputTokens:  t.InputTokens,
				OutputTokens: t.OutputTokens,
				TotalTokens:  t.TotalTokens,
			}
		case *StopToken:
			finishReason = t.Reason
		case *ErrorToken:
			streamErr = t.Err
		}

		s.tokens <- token
	}

	if streamErr != nil {
		s.span.RecordError(streamErr)
	}
	s.span.End(&llmtrace.Result{
		Completion:   completion.String(),
		Usage:        usage,
		FinishReason: finishReason,
	})
	close(s.tokens)
}
