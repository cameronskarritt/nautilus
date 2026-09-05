package openai

import (
	"bytes"
	"encoding/json"
	"fmt"

	"nautilus/internal/ai/llm"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

type CompletionsRequest struct {
	Model          enums.Model       `json:"model"`
	Messages       []message         `json:"messages"`
	Tools          []tool            `json:"tools,omitempty"`
	ToolChoice     any               `json:"tool_choice,omitempty"`
	ResponseFormat *structuredOutput `json:"response_format,omitempty"`

	Stream bool `json:"stream"`

	TopP             optional.Optional[float64] `json:"top_p,omitzero"`
	FrequencyPenalty optional.Optional[float64] `json:"frequency_penalty,omitzero"`
	Temperature      optional.Optional[float64] `json:"temperature,omitzero"`
	MaxTokens        optional.Optional[int]     `json:"max_completion_tokens,omitzero"`
	StopSequence     optional.Optional[string]  `json:"stop,omitzero"`
	PresencePenalty  optional.Optional[float64] `json:"presence_penalty,omitzero"`
	Verbosity        optional.Optional[string]  `json:"verbosity,omitzero"`
}

type CompletionEvent struct {
	ID                string      `json:"id"`
	Object            string      `json:"object"`
	Model             enums.Model `json:"model"`
	SystemFingerprint string      `json:"system_fingerprint"`
	Choices           []Choice    `json:"choices"`
}

type CompletionResponse struct {
	ID                string           `json:"id"`
	Object            string           `json:"object"`
	Model             enums.Model      `json:"model"`
	SystemFingerprint string           `json:"system_fingerprint"`
	Choices           []FullChoice     `json:"choices"`
	Usage             *completionUsage `json:"usage,omitempty"`
}

type completionUsage struct {
	InputTokens  int `json:"prompt_tokens"`
	OutputTokens int `json:"completion_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type Choice struct {
	Index        int     `json:"index"`
	Delta        message `json:"delta"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type FullChoice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type message struct {
	Role       enums.Role `json:"role"`
	Content    any        `json:"content,omitempty"` // string or []contentPart
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type tool struct {
	Type     string         `json:"type"`
	Function toolCallSchema `json:"function"`
}

type toolCall struct {
	ID       string           `json:"id"`
	Index    int              `json:"index"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type structuredOutput struct {
	Type   string                `json:"type"`
	Schema *llm.StructuredOutput `json:"json_schema,omitempty"`
}

type toolCallSchema struct {
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`

	Parameters parameters `json:"parameters,omitempty"`
}

type parameters struct {
	Type string `json:"type,omitempty"`
	*llm.Schema
}

func completionsRequest(request *llm.Request, opts *RequestOptions) *CompletionsRequest {
	return completionsRequestWithStream(request, opts, true)
}

func completionsRequestWithStream(request *llm.Request, opts *RequestOptions, stream bool) *CompletionsRequest {
	messages := make([]message, 0, len(request.Messages))
	for _, m := range request.Messages {
		if m.Role == enums.RoleTool {
			for _, tc := range m.ToolCalls {
				msg := message{
					Role:       enums.RoleTool,
					Content:    tc.Result,
					ToolCallID: tc.ID,
				}
				messages = append(messages, msg)
			}
			continue
		}

		toolCalls := make([]toolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			toolCalls = append(toolCalls, toolCall{
				ID:   tc.ID,
				Type: "function",
				Function: toolCallFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}

		msg := message{
			Role:      m.Role,
			Content:   m.Content,
			ToolCalls: toolCalls,
		}

		if len(m.Attachments) > 0 {
			// Build content array with attachments
			var parts []contentPart

			// Note: Attachments placed before text (images first)
			for _, att := range m.Attachments {
				mediaType := att.MediaType
				if mediaType == "" {
					mediaType = "image/jpeg" // default
				}
				detail := att.Detail
				if detail == "" {
					detail = "auto" // default
				}

				url := att.URL
				if att.Base64 != "" {
					url = fmt.Sprintf("data:%s;base64,%s", mediaType, att.Base64)
				}

				parts = append(parts, contentPart{
					Type: "image_url",
					ImageURL: &imageURL{
						URL:    url,
						Detail: detail,
					},
				})
			}

			if m.Content != "" {
				parts = append(parts, contentPart{
					Type: "text",
					Text: m.Content,
				})
			}

			msg.Content = parts
		}

		messages = append(messages, msg)
	}

	schemas := make([]tool, len(request.Tools))
	for i, t := range request.Tools {
		params := parameters{
			Type:   "object",
			Schema: t.Parameters,
		}

		schemas[i] = tool{
			Type: "function",
			Function: toolCallSchema{
				Type:        "object",
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		}
	}

	var format *structuredOutput
	if request.StructuredOutput != nil {
		format = &structuredOutput{
			Type:   "json_schema",
			Schema: request.StructuredOutput,
		}
	}

	var toolChoice any
	if request.ToolChoice != nil {
		tc, err := json.Marshal(request.ToolChoice)
		if err == nil {
			_ = json.Unmarshal(tc, &toolChoice)
		}
	}

	return &CompletionsRequest{
		Model:      request.Model,
		Messages:   messages,
		Tools:      schemas,
		ToolChoice: toolChoice,
		Stream:     stream,

		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		StopSequence:     opts.StopSequence,
		PresencePenalty:  opts.PresencePenalty,
		ResponseFormat:   format,
	}
}

func parseCompletionResponse(resp *CompletionResponse) (*llm.Message, error) {
	if len(resp.Choices) == 0 {
		return nil, errors.New("received completion response with no choices")
	}
	choice := resp.Choices[0]

	// Response content is always a string
	content, _ := choice.Message.Content.(string)

	msg := &llm.Message{
		Role:    enums.RoleAssistant,
		Content: content,
	}

	// Convert tool calls
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls := make([]llm.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		msg.ToolCalls = toolCalls
	}

	// Add usage if available
	if resp.Usage != nil {
		msg.Usage = &llm.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		}
	}

	return msg, nil
}

func handleCompletionEvent(sse *llm.ServerSentEvent) (llm.Token, error) {
	if bytes.Equal(sse.Data, []byte("[DONE]")) {
		return nil, nil
	}

	event := new(CompletionEvent)
	if err := json.Unmarshal(sse.Data, event); err != nil {
		return nil, errors.Wrap(err, "error unmarshaling OpenAI completion event")
	}

	if len(event.Choices) == 0 {
		return nil, errors.New("received completion event with no delta")
	}
	choice := event.Choices[0]

	if choice.FinishReason != "" {
		return &llm.StopToken{
			Type:   enums.TokenTypeStop,
			Reason: choice.FinishReason,
		}, nil
	}

	if len(choice.Delta.ToolCalls) > 0 {
		for _, toolCall := range choice.Delta.ToolCalls {
			token := &llm.ToolCallToken{
				Type:      enums.TokenTypeToolCall,
				ID:        toolCall.ID,
				Index:     toolCall.Index,
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			}
			return token, nil
		}
	}

	// Delta content is always a string
	if content, ok := choice.Delta.Content.(string); ok && content != "" {
		token := &llm.TextToken{
			Type: enums.TokenTypeText,
			Text: content,
		}
		return token, nil
	}

	return nil, nil
}
