package otel

import (
	"context"

	"nautilus/internal/observability/llmtrace"
	"nautilus/internal/observability/tracer"
)

const defaultOperation = "chat"

var _ llmtrace.LLMTracer = (*Tracer)(nil)
var _ llmtrace.Span = (*Span)(nil)

type Tracer struct {
	tracer tracer.Tracer
}

func NewTracer(t tracer.Tracer) *Tracer {
	return &Tracer{tracer: t}
}

func (t *Tracer) Start(ctx context.Context, call *llmtrace.Call) (context.Context, llmtrace.Span) {
	if t == nil || t.tracer == nil {
		return ctx, noopSpan{}
	}
	if call == nil {
		call = &llmtrace.Call{}
	}

	operation := call.Operation
	if operation == "" {
		operation = defaultOperation
	}

	ctx, span := t.tracer.Start(ctx, "llm."+operation, tracer.WithStartAttributes(callAttributes(call, operation)...))
	return ctx, &Span{span: span}
}

type Span struct {
	span   tracer.Span
	failed bool
}

func (s *Span) End(result *llmtrace.Result) {
	if s == nil || s.span == nil {
		return
	}

	if attrs := resultAttributes(result); len(attrs) > 0 {
		s.span.SetAttributes(attrs...)
	}
	if !s.failed {
		s.span.SetStatus(tracer.StatusOk, "")
	}
	s.span.End()
}

func (s *Span) RecordError(err error) {
	if s == nil || s.span == nil || err == nil {
		return
	}

	s.failed = true
	s.span.RecordError(err)
	s.span.SetStatus(tracer.StatusError, err.Error())
}

type noopSpan struct{}

func (s noopSpan) End(_ *llmtrace.Result) {}

func (s noopSpan) RecordError(_ error) {}

func callAttributes(call *llmtrace.Call, operation string) []tracer.Attribute {
	attrs := []tracer.Attribute{
		tracer.StringAttr("gen_ai.operation.name", operation),
	}
	if call.Provider != "" {
		attrs = append(attrs, tracer.StringAttr("gen_ai.system", call.Provider))
	}
	if call.Model != "" {
		attrs = append(attrs, tracer.StringAttr("gen_ai.request.model", call.Model))
	}
	if call.Prompt != "" {
		attrs = append(attrs, tracer.StringAttr("gen_ai.prompt", call.Prompt))
	}
	if call.Streaming {
		attrs = append(attrs, tracer.BoolAttr("llm.streaming", true))
	}
	return attrs
}

func resultAttributes(result *llmtrace.Result) []tracer.Attribute {
	if result == nil {
		return nil
	}

	var attrs []tracer.Attribute
	if result.Completion != "" {
		attrs = append(attrs, tracer.StringAttr("gen_ai.completion", result.Completion))
	}
	if result.FinishReason != "" {
		attrs = append(attrs, tracer.StringAttr("gen_ai.response.finish_reason", result.FinishReason))
	}
	if result.Usage != nil {
		attrs = append(attrs,
			tracer.IntAttr("gen_ai.usage.input_tokens", result.Usage.InputTokens),
			tracer.IntAttr("gen_ai.usage.output_tokens", result.Usage.OutputTokens),
			tracer.IntAttr("gen_ai.usage.total_tokens", result.Usage.TotalTokens),
		)
		if result.Usage.CachedInputTokens > 0 {
			attrs = append(attrs, tracer.IntAttr("gen_ai.usage.input_tokens.cached", result.Usage.CachedInputTokens))
		}
		if result.Usage.ReasoningOutputTokens > 0 {
			attrs = append(attrs, tracer.IntAttr("gen_ai.usage.output_tokens.reasoning", result.Usage.ReasoningOutputTokens))
		}
	}
	return attrs
}
