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

type ResponsesRequest struct {
	Model            enums.Model                  `json:"model"`
	Input            []any                        `json:"input"`
	Tools            []responseTool               `json:"tools,omitempty"`
	ToolChoice       any                          `json:"tool_choice,omitempty"`
	ResponseFormat   *structuredOutput            `json:"response_format,omitempty"`
	Stream           bool                         `json:"stream"`
	Reasoning        optional.Optional[Reasoning] `json:"reasoning,omitzero"`
	TopP             optional.Optional[float64]   `json:"top_p,omitzero"`
	FrequencyPenalty optional.Optional[float64]   `json:"frequency_penalty,omitzero"`
	Temperature      optional.Optional[float64]   `json:"temperature,omitzero"`
	MaxTokens        optional.Optional[int]       `json:"max_completion_tokens,omitzero"`
	StopSequence     optional.Optional[string]    `json:"stop,omitzero"`
	PresencePenalty  optional.Optional[float64]   `json:"presence_penalty,omitzero"`
	Verbosity        optional.Optional[string]    `json:"verbosity,omitzero"`
}

type functionCallOutputItem struct {
	Type       string      `json:"type"`
	ToolCallID string      `json:"call_id"`
	Output     []inputText `json:"output"`
}

type inputText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseTool struct {
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Parameters  responseParameters `json:"parameters"`
}

type responseParameters struct {
	Type string `json:"type,omitempty"`
	*llm.Schema
}

type ResponseEvent struct {
	Type           string    `json:"type"`
	SequenceNumber int       `json:"sequence_number"`
	Response       *Response `json:"response,omitempty"`
	OutputIndex    *int      `json:"output_index,omitempty"`
	ItemID         string    `json:"item_id,omitempty"`
	ContentIndex   *int      `json:"content_index,omitempty"`
	Delta          string    `json:"delta,omitempty"`
	Text           string    `json:"text,omitempty"`
	Item           *Item     `json:"item,omitempty"`
	// Function call fields
	Arguments   string `json:"arguments,omitempty"`
	CallID      string `json:"call_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Obfuscation string `json:"obfuscation,omitempty"`
}

type Response struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Status string `json:"status"`
	Model  string `json:"model"`
	Output []Item `json:"output"`
	Usage  *Usage `json:"usage,omitempty"`
}

type SummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Item struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Status  string        `json:"status,omitempty"`
	Content []Content     `json:"content,omitempty"`
	Role    string        `json:"role,omitempty"`
	Text    string        `json:"text,omitempty"`
	Summary []SummaryPart `json:"summary,omitempty"`
	// Function call fields
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
}

type Content struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Annotations []any  `json:"annotations,omitempty"`
	Logprobs    []any  `json:"logprobs,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type toolCallInfo struct {
	ID   string
	Name string
}

func responsesEventHandler() func(*llm.ServerSentEvent) (llm.Token, error) {
	toolCalls := make(map[string]toolCallInfo)

	return func(sse *llm.ServerSentEvent) (llm.Token, error) {
		if bytes.Equal(sse.Data, []byte("[DONE]")) {
			return nil, nil
		}

		event := new(ResponseEvent)
		if err := json.Unmarshal(sse.Data, event); err != nil {
			return nil, errors.Wrap(err, "error unmarshaling OpenAI response event")
		}

		//fmt.Println(string(sse.Data))

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				return &llm.TextToken{
					Type: enums.TokenTypeText,
					Text: event.Delta,
				}, nil
			}

		case "response.completed":
			// Check if usage information is available and return it
			if event.Response != nil && event.Response.Usage != nil {
				return &llm.UsageToken{
					Type:         enums.TokenTypeUsage,
					InputTokens:  event.Response.Usage.InputTokens,
					OutputTokens: event.Response.Usage.OutputTokens,
					TotalTokens:  event.Response.Usage.TotalTokens,
				}, nil
			}
			return &llm.StopToken{
				Type:   enums.TokenTypeStop,
				Reason: "completed",
			}, nil

		case "response.output_text.done":
			// Text block finished - don't emit stop, more content may follow
			return nil, nil

		case "response.output_item.added":
			// Check if this is a function call item
			if event.Item != nil && event.Item.Type == "function_call" {
				// Store tool call info for later use
				toolCalls[event.Item.ID] = toolCallInfo{
					ID:   event.Item.CallID,
					Name: event.Item.Name,
				}
				// Function call started - we'll wait for arguments
				return nil, nil
			}

		case "response.function_call_arguments.delta":
			// Function call arguments delta - return tool call token
			if event.Delta != "" && event.ItemID != "" {
				// Get stored tool call info
				info, exists := toolCalls[event.ItemID]
				if !exists {
					return nil, errors.Errorf("tool call info not found for item ID: %s", event.ItemID)
				}

				index := 0
				if event.OutputIndex != nil {
					index = *event.OutputIndex
				}
				return &llm.ToolCallToken{
					Type:      enums.TokenTypeToolCall,
					ID:        info.ID,
					Index:     index,
					Name:      info.Name,
					Arguments: event.Delta,
				}, nil
			}

		case "response.function_call_arguments.done":
			// Arguments complete - don't emit stop, more function calls may follow
			return nil, nil

		case "response.output_item.done":
			// Clean up tracking, but don't emit stop
			if event.Item != nil && event.Item.Type == "function_call" {
				delete(toolCalls, event.Item.ID)
			}
			return nil, nil

		case "response.reasoning_summary_text.delta":
			if event.Delta != "" {
				return &llm.TextToken{
					Type: enums.TokenTypeReasoning,
					Text: event.Delta,
				}, nil
			}

		case "response.failed":
			// Response failed - emit stop token
			return &llm.StopToken{
				Type:   enums.TokenTypeStop,
				Reason: "failed",
			}, nil

		case "response.incomplete":
			// Response incomplete - emit stop token
			return &llm.StopToken{
				Type:   enums.TokenTypeStop,
				Reason: "incomplete",
			}, nil

		default:
			// For other event types (created, in_progress, etc.), return nil
			// This allows the stream to continue without generating tokens
			return nil, nil
		}

		return nil, nil
	}
}

func responsesRequest(request *llm.Request, opts *RequestOptions) *ResponsesRequest {
	return responsesRequestWithStream(request, opts, true)
}

func responsesRequestWithStream(request *llm.Request, opts *RequestOptions, stream bool) *ResponsesRequest {
	// The responses API uses items, not messages
	// We need to convert the conversation to the proper format
	items := make([]any, 0)

	for _, m := range request.Messages {
		if m.Role == enums.RoleTool {
			// Convert tool messages to function_call_output items
			for _, tc := range m.ToolCalls {
				functionCallOutput := functionCallOutputItem{
					Type:       "function_call_output",
					ToolCallID: tc.ID,
					Output: []inputText{
						{
							Type: "input_text",
							Text: tc.Result,
						},
					},
				}
				items = append(items, functionCallOutput)
			}
			continue
		}

		// Handle assistant messages with tool calls by separating content and tool calls
		if m.Role == enums.RoleAssistant && len(m.ToolCalls) > 0 {
			// Include the assistant message content as a separate message item
			if m.Content != "" {
				messageItem := map[string]any{
					"type":    "message",
					"role":    string(m.Role),
					"content": m.Content,
				}
				items = append(items, messageItem)
			}

			// Include each tool call as a separate function_call item
			for _, tc := range m.ToolCalls {
				functionCallItem := map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				}
				items = append(items, functionCallItem)
			}
			continue
		}

		// For regular messages (user, assistant without tool calls), include them as message items
		messageItem := map[string]any{
			"type":    "message",
			"role":    string(m.Role),
			"content": m.Content,
		}

		if len(m.Attachments) > 0 {
			// Build content array with attachments
			var contentParts []any

			// Note: Attachments placed before text (images first)
			for _, att := range m.Attachments {
				mediaType := att.MediaType
				if mediaType == "" {
					mediaType = "image/jpeg" // default
				}

				var url string
				if att.Base64 != "" {
					url = fmt.Sprintf("data:%s;base64,%s", mediaType, att.Base64)
				} else {
					url = att.URL
				}

				contentParts = append(contentParts, map[string]any{
					"type":      "input_image",
					"image_url": url,
				})
			}

			if m.Content != "" {
				contentParts = append(contentParts, map[string]any{
					"type": "input_text",
					"text": m.Content,
				})
			}

			messageItem["content"] = contentParts
		}

		items = append(items, messageItem)
	}

	schemas := make([]responseTool, len(request.Tools))
	for i, t := range request.Tools {
		params := responseParameters{
			Type:   "object",
			Schema: t.Parameters,
		}

		schemas[i] = responseTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
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
		// Responses API uses a different format for specific function tool choice
		// String values ("none", "auto", "required") are the same
		// But specific function uses {"type": "function", "name": "..."} instead of
		// {"type": "function", "function": {"name": "..."}}
		switch request.ToolChoice.Mode {
		case "none", "auto", "required":
			toolChoice = request.ToolChoice.Mode
		case "function":
			toolChoice = map[string]string{
				"type": "function",
				"name": request.ToolChoice.Name,
			}
		default:
			toolChoice = "auto"
		}
	}

	return &ResponsesRequest{
		Model:      request.Model,
		Input:      items,
		Tools:      schemas,
		ToolChoice: toolChoice,
		Stream:     stream,

		Reasoning:        opts.Reasoning,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		StopSequence:     opts.StopSequence,
		PresencePenalty:  opts.PresencePenalty,
		ResponseFormat:   format,
	}
}

func parseResponsesResponse(resp *Response) *llm.Message {
	msg := &llm.Message{
		Role:    enums.RoleAssistant,
		Content: "",
	}

	var toolCalls []llm.ToolCall
	var reasoningParts []string

	// Parse output items
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			// Extract text content from message items
			for _, content := range item.Content {
				if (content.Type == "text" || content.Type == "output_text") && content.Text != "" {
					msg.Content += content.Text
				}
			}
			if item.Text != "" {
				msg.Content += item.Text
			}

		case "function_call":
			// Extract function call information
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})

		case "reasoning":
			// Extract reasoning summary
			for _, part := range item.Summary {
				if part.Type == "text" || part.Type == "summary_text" {
					reasoningParts = append(reasoningParts, part.Text)
				}
			}
		}
	}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	if len(reasoningParts) > 0 {
		msg.Reasoning = ""
		for _, part := range reasoningParts {
			msg.Reasoning += part
		}
	}

	// Add usage if available
	if resp.Usage != nil {
		msg.Usage = &llm.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		}
	}

	return msg
}
