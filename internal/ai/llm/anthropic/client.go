package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"nautilus/internal/ai/llm"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/observability/llmtrace"
	"nautilus/internal/observability/llmtrace/noop"
	"nautilus/internal/optional"
)

const (
	MessagesEndpoint = "https://api.anthropic.com/v1/messages"
	APIVersion       = "2023-06-01"

	ClaudeSonnet45 enums.Model = "claude-sonnet-4-5-20250929"
	ClaudeHaiku45  enums.Model = "claude-haiku-4-5-20251001"
	ClaudeOpus45   enums.Model = "claude-opus-4-5-20251101"
)

type Client struct {
	key               string
	client            *http.Client
	completionTimeout time.Duration
	opts              *RequestOptions
	endpoint          string
	headers           http.Header
	llmTracer         llmtrace.LLMTracer
}

// Thinking configures extended thinking for Claude models.
type Thinking struct {
	BudgetTokens int `json:"budget_tokens"`
}

type RequestOptions struct {
	Endpoint    optional.Optional[string]   `json:"endpoint,omitzero"`
	Temperature optional.Optional[float64]  `json:"temperature,omitzero"`
	TopP        optional.Optional[float64]  `json:"top_p,omitzero"`
	MaxTokens   optional.Optional[int]      `json:"max_tokens,omitzero"`
	TopK        optional.Optional[int]      `json:"top_k,omitzero"`
	Thinking    optional.Optional[Thinking] `json:"thinking,omitzero"`
}

func defaultRequestOptions() *RequestOptions {
	return &RequestOptions{
		Temperature: optional.Set(1.0),
		MaxTokens:   optional.Set(4096),
	}
}

func NewClient(opts *RequestOptions) *Client {
	if opts == nil {
		opts = defaultRequestOptions()
	}
	headers := make(http.Header)
	headers.Set("anthropic-version", APIVersion)
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "nautilus/1.0")

	return &Client{
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
		completionTimeout: 2 * time.Minute,
		opts:              opts,
		endpoint:          MessagesEndpoint,
		headers:           headers,
		llmTracer:         noop.NewTracer(),
	}
}

func (c *Client) WithEndpoint(url string) *Client {
	c.endpoint = url
	return c
}

func (c *Client) WithAPIKey(key string) *Client {
	c.key = key
	c.headers.Set("x-api-key", key)
	return c
}

func (c *Client) WithTransport(transport http.RoundTripper) *Client {
	c.client.Transport = transport
	return c
}

func (c *Client) WithTimeout(timeout time.Duration) *Client {
	c.client.Timeout = timeout
	c.completionTimeout = timeout
	return c
}

func (c *Client) WithHeaders(headers http.Header) *Client {
	for key, values := range headers {
		for _, value := range values {
			c.headers.Set(key, value)
		}
	}
	return c
}

func (c *Client) WithLLMTracer(tracer llmtrace.LLMTracer) *Client {
	if tracer == nil {
		tracer = noop.NewTracer()
	}
	c.llmTracer = tracer
	return c
}

func (c *Client) StreamCompletion(ctx context.Context, request *llm.Request) (llm.TokenStream, error) {
	ctx, span := c.llmTracer.Start(ctx, traceCall(request, true))
	req := buildRequest(request, c.opts, true)

	b, err := json.Marshal(req)
	if err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error marshaling request")
	}
	payload := bytes.NewReader(b)

	post, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, payload)
	if err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error creating request")
	}
	post.Header = c.headers.Clone()

	resp, err := c.client.Do(post)
	if err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error making request")
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		apiErr := new(APIError)
		if err := httputil.DecodeJSON(resp.Body, &apiErr); err != nil {
			recordTraceError(span, err)
			return nil, errors.Wrap(err, "error decoding API error")
		}

		err := ClassifyError(resp, apiErr)
		recordTraceError(span, err)
		return nil, err
	}

	stream := llm.NewEventStream(nil)
	return llm.TraceTokenStream(stream.HandleEvents(ctx, resp, messagesEventHandler()), span), nil
}

func (c *Client) Completion(ctx context.Context, request *llm.Request) (*llm.Message, error) {
	ctx, span := c.llmTracer.Start(ctx, traceCall(request, false))
	startTime := time.Now()

	req := buildRequest(request, c.opts, false)

	b, err := json.Marshal(req)
	if err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error marshaling request")
	}
	payload := bytes.NewReader(b)

	post, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, payload)
	if err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error creating request")
	}
	post.Header = c.headers.Clone()

	resp, err := c.client.Do(post)
	if err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error making request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := new(APIError)
		if err := httputil.DecodeJSON(resp.Body, &apiErr); err != nil {
			recordTraceError(span, err)
			return nil, errors.Wrap(err, "error decoding API error")
		}

		err := ClassifyError(resp, apiErr)
		recordTraceError(span, err)
		return nil, err
	}

	var response MessagesResponse
	if err := httputil.DecodeJSON(resp.Body, &response); err != nil {
		recordTraceError(span, err)
		return nil, errors.Wrap(err, "error decoding response")
	}

	msg := parseMessagesResponse(&response)
	endTime := time.Now()

	// Populate metrics
	var inputTokens, outputTokens int
	if msg.Usage != nil {
		inputTokens = msg.Usage.InputTokens
		outputTokens = msg.Usage.OutputTokens
	}

	msg.Metrics = &llm.Metrics{
		StartTime:        startTime,
		FirstTokenTime:   startTime,
		FirstContentTime: startTime,
		EndTime:          endTime,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
	}

	span.End(traceResult(msg, response.StopReason))
	return msg, nil
}

func traceCall(request *llm.Request, streaming bool) *llmtrace.Call {
	call := &llmtrace.Call{
		Operation: "chat",
		Provider:  string(enums.ProviderAnthropic),
		Streaming: streaming,
	}
	if request == nil {
		return call
	}
	call.Model = string(request.Model)
	if b, err := json.Marshal(request); err == nil {
		call.Prompt = string(b)
	}
	return call
}

func traceResult(msg *llm.Message, finishReason string) *llmtrace.Result {
	result := &llmtrace.Result{FinishReason: finishReason}
	if msg == nil {
		return result
	}
	if b, err := json.Marshal(msg); err == nil {
		result.Completion = string(b)
	} else {
		result.Completion = msg.Content
	}
	if msg.Usage != nil {
		result.Usage = &llmtrace.Usage{
			InputTokens:  msg.Usage.InputTokens,
			OutputTokens: msg.Usage.OutputTokens,
			TotalTokens:  msg.Usage.TotalTokens,
		}
	}
	return result
}

func recordTraceError(span llmtrace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.End(nil)
}
