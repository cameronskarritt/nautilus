package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	ChatCompletionsEndpoint = "https://api.openai.com/v1/chat/completions"
	ResponsesEndpoint       = "https://api.openai.com/v1/responses"

	GPT5     enums.Model = "gpt-5"
	GPT5Mini enums.Model = "gpt-5-mini"
	GPT5Nano enums.Model = "gpt-5-nano"
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

type Reasoning struct {
	Effort  optional.Optional[string] `json:"effort,omitzero"`
	Summary optional.Optional[string] `json:"summary,omitzero"`
}

type RequestOptions struct {
	Endpoint        optional.Optional[string] `json:"endpoint,omitzero"`
	UseResponsesAPI bool                      `json:"use_responses_api"`

	Reasoning        optional.Optional[Reasoning] `json:"reasoning,omitzero"`
	Temperature      optional.Optional[float64]   `json:"temperature,omitzero"`
	TopP             optional.Optional[float64]   `json:"top_p,omitzero"`
	MaxTokens        optional.Optional[int]       `json:"max_completion_tokens,omitzero"`
	StopSequence     optional.Optional[string]    `json:"stop,omitzero"`
	PresencePenalty  optional.Optional[float64]   `json:"presence_penalty,omitzero"`
	FrequencyPenalty optional.Optional[float64]   `json:"frequency_penalty,omitzero"`
}

func defaultRequestOptions() *RequestOptions {
	return &RequestOptions{
		Temperature:     optional.Set(1.0),
		Endpoint:        optional.Set(ResponsesEndpoint),
		UseResponsesAPI: true,
	}
}

func NewClient(opts *RequestOptions) *Client {
	if opts == nil {
		opts = defaultRequestOptions()
	}
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "nautilus/1.0")

	return &Client{
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
		completionTimeout: 2 * time.Minute,
		opts:              opts,
		endpoint:          ResponsesEndpoint,
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
	c.headers.Set("Authorization", fmt.Sprintf("Bearer %s", key))
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
	if c.key == "" {
		return nil, errors.New("no API key configured")
	}

	ctx, span := c.llmTracer.Start(ctx, traceCall(request, true))

	var req any
	if c.opts.UseResponsesAPI {
		req = responsesRequest(request, c.opts)
	} else {
		req = completionsRequest(request, c.opts)
	}

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

	var handler llm.EventHandler = handleCompletionEvent
	if c.opts.UseResponsesAPI {
		handler = responsesEventHandler()
	}

	stream := llm.NewEventStream(nil)
	return llm.TraceTokenStream(stream.HandleEvents(ctx, resp, handler), span), nil
}

func (c *Client) Completion(ctx context.Context, request *llm.Request) (*llm.Message, error) {
	if c.key == "" {
		return nil, errors.New("no API key configured")
	}

	ctx, span := c.llmTracer.Start(ctx, traceCall(request, false))
	startTime := time.Now()

	var req any
	if c.opts.UseResponsesAPI {
		req = responsesRequestWithStream(request, c.opts, false)
	} else {
		req = completionsRequestWithStream(request, c.opts, false)
	}

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

	var msg *llm.Message
	var finishReason string

	// Parse response based on format
	if c.opts.UseResponsesAPI {
		var response Response
		if err := httputil.DecodeJSON(resp.Body, &response); err != nil {
			recordTraceError(span, err)
			return nil, errors.Wrap(err, "error decoding response")
		}
		finishReason = response.Status
		msg = parseResponsesResponse(&response)
	} else {
		var response CompletionResponse
		if err := httputil.DecodeJSON(resp.Body, &response); err != nil {
			recordTraceError(span, err)
			return nil, errors.Wrap(err, "error decoding response")
		}
		if len(response.Choices) > 0 {
			finishReason = response.Choices[0].FinishReason
		}
		msg, err = parseCompletionResponse(&response)
	}

	if err != nil {
		recordTraceError(span, err)
		return nil, err
	}

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

	span.End(traceResult(msg, finishReason))
	return msg, nil
}

func traceCall(request *llm.Request, streaming bool) *llmtrace.Call {
	call := &llmtrace.Call{
		Operation: "chat",
		Provider:  string(enums.ProviderOpenAI),
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
