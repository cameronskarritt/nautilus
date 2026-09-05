package tracer

import (
	"context"

	"nautilus/internal/ai/llm"
)

var _ llm.Client = (*TracedLLMClient)(nil)

type TracedLLMClient struct {
	client llm.Client
	tracer Tracer
}

func NewTracedLLMClient(client llm.Client, t Tracer) *TracedLLMClient {
	return &TracedLLMClient{client: client, tracer: t}
}

func (c *TracedLLMClient) Completion(ctx context.Context, req *llm.Request) (*llm.Message, error) {
	ctx, span := c.tracer.Start(ctx, "llm.completion")
	defer span.End()

	span.SetAttributes(StringAttr("llm.model", string(req.Model)))

	msg, err := c.client.Completion(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return msg, err
}

func (c *TracedLLMClient) StreamCompletion(ctx context.Context, req *llm.Request) (llm.TokenStream, error) {
	ctx, span := c.tracer.Start(ctx, "llm.stream_completion")
	defer span.End()

	span.SetAttributes(StringAttr("llm.model", string(req.Model)))

	stream, err := c.client.StreamCompletion(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return stream, err
}
