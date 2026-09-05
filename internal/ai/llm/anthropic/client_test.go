package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"nautilus/internal/ai/llm"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/observability/llmtrace"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		request      *llm.Request
		wantContent  string
		wantTools    []llm.ToolCall
		wantUsage    llm.Usage
		finishReason string
	}{
		{
			name:         "text",
			path:         "testdata/completion_text.jsonl",
			request:      textRequest(),
			wantContent:  "Hello.",
			wantUsage:    llm.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
			finishReason: "end_turn",
		},
		{
			name:    "tool calls",
			path:    "testdata/completion_tool_calls.jsonl",
			request: toolRequest(),
			wantTools: []llm.ToolCall{
				{ID: "call_weather", Name: "get_weather", Arguments: `{"location":"San Francisco"}`},
				{ID: "call_time", Name: "get_time", Arguments: `{"timezone":"America/New_York"}`},
			},
			wantUsage:    llm.Usage{InputTokens: 10, OutputTokens: 8, TotalTokens: 18},
			finishReason: "tool_use",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, recorder, tracer := newCassetteClient(t, tt.path)
			msg, err := client.Completion(t.Context(), tt.request)
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.NotNil(t, msg.Metrics)

			require.Equal(t, enums.RoleAssistant, msg.Role)
			require.Equal(t, tt.wantContent, msg.Content)
			require.Equal(t, tt.wantTools, msg.ToolCalls)
			require.Equal(t, &tt.wantUsage, msg.Usage)
			require.Equal(t, tt.wantUsage.InputTokens, msg.Metrics.InputTokens)
			require.Equal(t, tt.wantUsage.OutputTokens, msg.Metrics.OutputTokens)
			assertRequest(t, recorder, tt.request, false)
			assertSpan(t, tracer, tt.request, false, tt.finishReason, &tt.wantUsage)
		})
	}
}

func TestStreamCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		request     *llm.Request
		wantContent string
		wantTools   []llm.ToolCall
		wantUsage   llm.Usage
		wantStop    string
	}{
		{
			name:        "text",
			path:        "testdata/stream_text.jsonl",
			request:     textRequest(),
			wantContent: "Hello.",
			wantUsage:   llm.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
			wantStop:    "end_turn",
		},
		{
			name:    "tool calls",
			path:    "testdata/stream_tool_calls.jsonl",
			request: toolRequest(),
			wantTools: []llm.ToolCall{
				{ID: "call_weather", Name: "get_weather", Arguments: `{"location":"San Francisco"}`},
				{ID: "call_time", Name: "get_time", Arguments: `{"timezone":"America/New_York"}`},
			},
			wantUsage: llm.Usage{InputTokens: 10, OutputTokens: 8, TotalTokens: 18},
			wantStop:  "end_turn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, recorder, tracer := newCassetteClient(t, tt.path)
			stream, err := client.StreamCompletion(t.Context(), tt.request)
			require.NoError(t, err)

			got := readStream(t, stream)
			require.Equal(t, tt.wantContent, got.content)
			require.Equal(t, tt.wantTools, got.tools)
			require.Equal(t, &tt.wantUsage, got.usage)
			require.Equal(t, tt.wantStop, got.stop)
			assertRequest(t, recorder, tt.request, true)
			assertSpan(t, tracer, tt.request, true, tt.wantStop, &tt.wantUsage)
		})
	}
}

func textRequest() *llm.Request {
	return &llm.Request{
		Model: ClaudeSonnet45,
		Messages: []llm.Message{{
			Role:    enums.RoleUser,
			Content: "Reply with exactly: Hello.",
		}},
	}
}

func toolRequest() *llm.Request {
	return &llm.Request{
		Model: ClaudeSonnet45,
		Messages: []llm.Message{{
			Role:    enums.RoleUser,
			Content: "Call get_weather for San Francisco and get_time for America/New_York. Do not write text.",
		}},
		Tools: []llm.Tool{
			{
				Name:        "get_weather",
				Description: "Get weather.",
				Parameters: &llm.Schema{
					Type:       llm.TypeObject,
					Properties: llm.Properties{"location": llm.S("City")},
					Required:   []string{"location"},
				},
			},
			{
				Name:        "get_time",
				Description: "Get time.",
				Parameters: &llm.Schema{
					Type:       llm.TypeObject,
					Properties: llm.Properties{"timezone": llm.S("Timezone")},
					Required:   []string{"timezone"},
				},
			},
		},
	}
}

func newCassetteClient(tb testing.TB, path string) (*Client, *requestRecorder, *recordingLLMTracer) {
	tb.Helper()

	cassette, err := testutil.LoadCassette(path)
	require.NoError(tb, err)
	recorder := &requestRecorder{
		next: httputil.NewReplayTransport(cassette, httputil.WithFastForward()),
	}
	tracer := new(recordingLLMTracer)
	client := NewClient(nil).
		WithAPIKey("fake-key").
		WithTransport(recorder).
		WithLLMTracer(tracer)
	return client, recorder, tracer
}

func assertRequest(tb testing.TB, recorder *requestRecorder, want *llm.Request, stream bool) {
	tb.Helper()

	var got MessagesRequest
	require.NoError(tb, json.Unmarshal(recorder.body, &got))
	require.Equal(tb, want.Model, got.Model)
	require.Equal(tb, 4096, got.MaxTokens)
	require.Equal(tb, stream, got.Stream)
	require.Len(tb, got.Messages, 1)
	require.Equal(tb, want.Messages[0].Role, got.Messages[0].Role)
	require.Equal(tb, want.Messages[0].Content, got.Messages[0].Content)
	require.Len(tb, got.Tools, len(want.Tools))
	for i := range want.Tools {
		require.Equal(tb, want.Tools[i].Name, got.Tools[i].Name)
		require.Equal(tb, want.Tools[i].Description, got.Tools[i].Description)
		wantParams, err := json.Marshal(want.Tools[i].Parameters)
		require.NoError(tb, err)
		gotParams, err := json.Marshal(got.Tools[i].InputSchema)
		require.NoError(tb, err)
		require.JSONEq(tb, string(wantParams), string(gotParams))
	}
	require.Equal(tb, "application/json", recorder.header.Get("Content-Type"))
	require.Equal(tb, APIVersion, recorder.header.Get("Anthropic-Version"))
	require.Equal(tb, "fake-key", recorder.header.Get("X-Api-Key"))
}

func readStream(tb testing.TB, stream llm.TokenStream) streamResult {
	tb.Helper()

	var result streamResult
	positions := make(map[string]int)
	for token := range stream.Tokens() {
		switch token := token.(type) {
		case *llm.TextToken:
			if token.Type == enums.TokenTypeText {
				result.content += token.Text
			}
		case *llm.ToolCallToken:
			pos, ok := positions[token.ID]
			if !ok {
				pos = len(result.tools)
				positions[token.ID] = pos
				result.tools = append(result.tools, llm.ToolCall{ID: token.ID, Name: token.Name})
			}
			result.tools[pos].Arguments += token.Arguments
		case *llm.UsageToken:
			result.usage = &llm.Usage{
				InputTokens:  token.InputTokens,
				OutputTokens: token.OutputTokens,
				TotalTokens:  token.TotalTokens,
			}
		case *llm.StopToken:
			result.stop = token.Reason
		case *llm.ErrorToken:
			require.NoError(tb, token.Err)
		}
	}
	return result
}

func assertSpan(tb testing.TB, tracer *recordingLLMTracer, req *llm.Request, streaming bool, finish string, usage *llm.Usage) {
	tb.Helper()

	span := tracer.onlySpan(tb)
	require.True(tb, span.ended)
	require.Nil(tb, span.err)
	require.Equal(tb, string(enums.ProviderAnthropic), span.call.Provider)
	require.Equal(tb, string(req.Model), span.call.Model)
	require.Equal(tb, streaming, span.call.Streaming)
	require.NotNil(tb, span.result)
	require.Equal(tb, finish, span.result.FinishReason)
	require.Equal(tb, &llmtrace.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}, span.result.Usage)
}

type streamResult struct {
	content string
	tools   []llm.ToolCall
	usage   *llm.Usage
	stop    string
}

type requestRecorder struct {
	next   http.RoundTripper
	body   []byte
	header http.Header
}

func (r *requestRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read request body")
	}
	r.body = body
	r.header = req.Header.Clone()
	req.Body = io.NopCloser(bytes.NewReader(body))
	resp, err := r.next.RoundTrip(req)
	return resp, errors.Wrap(err, "replay request")
}

type recordingLLMTracer struct {
	spans []*recordingLLMSpan
}

func (t *recordingLLMTracer) Start(ctx context.Context, call *llmtrace.Call) (context.Context, llmtrace.Span) {
	span := &recordingLLMSpan{}
	if call != nil {
		span.call = *call
	}
	t.spans = append(t.spans, span)
	return ctx, span
}

func (t *recordingLLMTracer) onlySpan(tb testing.TB) *recordingLLMSpan {
	tb.Helper()
	require.Len(tb, t.spans, 1)
	return t.spans[0]
}

type recordingLLMSpan struct {
	call   llmtrace.Call
	result *llmtrace.Result
	err    error
	ended  bool
}

func (s *recordingLLMSpan) End(result *llmtrace.Result) {
	s.result = result
	s.ended = true
}

func (s *recordingLLMSpan) RecordError(err error) {
	s.err = err
}
