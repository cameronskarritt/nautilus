package anthropic

import (
	"encoding/json"

	"nautilus/internal/ai/llm"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

// thinkingConfig configures extended thinking for the API request
type thinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// MessagesRequest is the request body for the Anthropic Messages API
type MessagesRequest struct {
	Model      enums.Model     `json:"model"`
	MaxTokens  int             `json:"max_tokens"`
	Messages   []message       `json:"messages"`
	System     string          `json:"system,omitempty"`
	Tools      []tool          `json:"tools,omitempty"`
	ToolChoice any             `json:"tool_choice,omitempty"`
	Stream     bool            `json:"stream"`
	Thinking   *thinkingConfig `json:"thinking,omitempty"`

	Temperature optional.Optional[float64] `json:"temperature,omitzero"`
	TopP        optional.Optional[float64] `json:"top_p,omitzero"`
	TopK        optional.Optional[int]     `json:"top_k,omitzero"`
}

type message struct {
	Role    enums.Role `json:"role"`
	Content any        `json:"content"` // string or []contentBlock
}

type contentBlock interface {
	isContentBlock()
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (textBlock) isContentBlock() {}

type toolUseBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

func (toolUseBlock) isContentBlock() {}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

func (toolResultBlock) isContentBlock() {}

type imageBlock struct {
	Type   string      `json:"type"` // "image"
	Source imageSource `json:"source"`
}

func (imageBlock) isContentBlock() {}

type imageSource struct {
	Type      string `json:"type"`           // "base64" or "url"
	MediaType string `json:"media_type"`     // e.g., "image/jpeg"
	Data      string `json:"data,omitempty"` // for base64
	URL       string `json:"url,omitempty"`  // for url type
}

type tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema inputSchema `json:"input_schema"`
}

type inputSchema struct {
	Type string `json:"type"`
	*llm.Schema
}

// MessagesResponse is the response from the Anthropic Messages API
type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []contentItem  `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
	Usage        *responseUsage `json:"usage"`
}

type contentItem struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Thinking string          `json:"thinking,omitempty"` // for thinking blocks
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

type responseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Streaming event types
type messageStartEvent struct {
	Type    string            `json:"type"`
	Message *MessagesResponse `json:"message"`
}

type contentBlockStartEvent struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock *contentItem `json:"content_block"`
}

type contentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta *delta `json:"delta"`
}

type delta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`  // for thinking_delta
	Signature   string `json:"signature,omitempty"` // for signature_delta
}

type messageDeltaEvent struct {
	Type  string        `json:"type"`
	Delta *messageDelta `json:"delta"`
	Usage *deltaUsage   `json:"usage"`
}

type messageDelta struct {
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence"`
}

type deltaUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// buildRequest converts an llm.Request to an Anthropic MessagesRequest
func buildRequest(req *llm.Request, opts *RequestOptions, stream bool) *MessagesRequest {
	var systemPrompt string
	var messages []message

	// Extract system message and convert messages
	for _, m := range req.Messages {
		if m.Role == enums.RoleSystem {
			systemPrompt = m.Content
			continue
		}

		if m.Role == enums.RoleTool {
			// Tool results go into a user message with tool_result content blocks
			var content []contentBlock
			for _, tc := range m.ToolCalls {
				content = append(content, toolResultBlock{
					Type:      "tool_result",
					ToolUseID: tc.ID,
					Content:   tc.Result,
				})
			}
			messages = append(messages, message{
				Role:    enums.RoleUser,
				Content: content,
			})
			continue
		}

		if m.Role == enums.RoleAssistant && len(m.ToolCalls) > 0 {
			// Assistant message with tool calls
			var content []contentBlock
			if m.Content != "" {
				content = append(content, textBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				// Parse arguments as JSON
				var input any
				if tc.Arguments == "" {
					// Empty arguments must be an empty object for Anthropic API
					input = map[string]any{}
				} else if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
					// Fallback to empty object if JSON is invalid
					input = map[string]any{}
				}
				content = append(content, toolUseBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			messages = append(messages, message{
				Role:    m.Role,
				Content: content,
			})
			continue
		}

		// Regular user or assistant message
		if len(m.Attachments) > 0 {
			// Build content array with attachments
			var content []contentBlock

			// Note: Attachments placed before text (images first)
			for _, att := range m.Attachments {
				mediaType := att.MediaType
				if mediaType == "" {
					mediaType = "image/jpeg" // default
				}
				source := imageSource{MediaType: mediaType}
				if att.Base64 != "" {
					source.Type = "base64"
					source.Data = att.Base64
				} else if att.URL != "" {
					source.Type = "url"
					source.URL = att.URL
				}
				content = append(content, imageBlock{Type: "image", Source: source})
			}

			if m.Content != "" {
				content = append(content, textBlock{Type: "text", Text: m.Content})
			}

			messages = append(messages, message{
				Role:    m.Role,
				Content: content,
			})
		} else {
			messages = append(messages, message{
				Role:    m.Role,
				Content: m.Content,
			})
		}
	}

	// Convert tools
	var tools []tool
	for _, t := range req.Tools {
		tools = append(tools, tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: inputSchema{
				Type:   "object",
				Schema: t.Parameters,
			},
		})
	}

	// Convert tool choice
	var toolChoice any
	if req.ToolChoice != nil {
		switch req.ToolChoice.Mode {
		case "none":
			// Anthropic doesn't support "none" directly, omit tools instead
			tools = nil
		case "auto":
			toolChoice = map[string]string{"type": "auto"}
		case "required":
			toolChoice = map[string]string{"type": "any"}
		case "function":
			toolChoice = map[string]string{
				"type": "tool",
				"name": req.ToolChoice.Name,
			}
		}
	}

	maxTokens := 4096
	if opts.MaxTokens.Set {
		maxTokens = opts.MaxTokens.Data
	}

	messagesReq := &MessagesRequest{
		Model:      req.Model,
		MaxTokens:  maxTokens,
		Messages:   messages,
		System:     systemPrompt,
		Tools:      tools,
		ToolChoice: toolChoice,
		Stream:     stream,
	}

	// Configure extended thinking if enabled
	// When thinking is enabled, temperature and topK cannot be modified
	if opts.Thinking.Set {
		messagesReq.Thinking = &thinkingConfig{
			Type:         "enabled",
			BudgetTokens: opts.Thinking.Data.BudgetTokens,
		}
		// TopP can only be 0.95-1.0 when thinking is enabled
		messagesReq.TopP = opts.TopP
	} else {
		messagesReq.Temperature = opts.Temperature
		messagesReq.TopP = opts.TopP
		messagesReq.TopK = opts.TopK
	}

	return messagesReq
}

func parseMessagesResponse(resp *MessagesResponse) *llm.Message {
	msg := &llm.Message{
		Role: enums.RoleAssistant,
	}

	var toolCalls []llm.ToolCall

	for _, item := range resp.Content {
		switch item.Type {
		case "thinking":
			msg.Reasoning += item.Thinking
		case "text":
			msg.Content += item.Text
		case "tool_use":
			args, err := json.Marshal(item.Input)
			if err != nil {
				continue
			}
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:        item.ID,
				Name:      item.Name,
				Arguments: string(args),
			})
		}
	}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	if resp.Usage != nil {
		msg.Usage = &llm.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}

	return msg
}

// messagesEventHandler handles streaming events from the Anthropic Messages API
func messagesEventHandler() llm.EventHandler {
	var currentToolUse *toolUseBlock
	var inThinkingBlock bool
	var inputTokens int

	return func(sse *llm.ServerSentEvent) (llm.Token, error) {
		eventType := string(sse.Event)

		switch eventType {
		case "message_start":
			var event messageStartEvent
			if err := json.Unmarshal(sse.Data, &event); err != nil {
				return nil, errors.Wrap(err, "error unmarshaling message_start")
			}
			if event.Message != nil && event.Message.Usage != nil {
				inputTokens = event.Message.Usage.InputTokens
			}
			return nil, nil

		case "content_block_start":
			var event contentBlockStartEvent
			if err := json.Unmarshal(sse.Data, &event); err != nil {
				return nil, errors.Wrap(err, "error unmarshaling content_block_start")
			}
			if event.ContentBlock == nil {
				return nil, nil
			}

			switch event.ContentBlock.Type {
			case "thinking":
				inThinkingBlock = true
				return nil, nil
			case "tool_use":
				currentToolUse = &toolUseBlock{
					Type:  "tool_use",
					ID:    event.ContentBlock.ID,
					Name:  event.ContentBlock.Name,
					Input: "",
				}
				// Return initial tool call token with name and ID
				return &llm.ToolCallToken{
					Type:      enums.TokenTypeToolCall,
					ID:        event.ContentBlock.ID,
					Index:     event.Index,
					Name:      event.ContentBlock.Name,
					Arguments: "",
				}, nil
			}
			return nil, nil

		case "content_block_delta":
			var event contentBlockDeltaEvent
			if err := json.Unmarshal(sse.Data, &event); err != nil {
				return nil, errors.Wrap(err, "error unmarshaling content_block_delta")
			}
			if event.Delta == nil {
				return nil, nil
			}

			switch event.Delta.Type {
			case "thinking_delta":
				if event.Delta.Thinking != "" {
					return &llm.TextToken{
						Type: enums.TokenTypeReasoning,
						Text: event.Delta.Thinking,
					}, nil
				}
			case "signature_delta":
				// Signature is used for verification when passing blocks back to API
				// We don't need to emit a token for it
				return nil, nil
			case "text_delta":
				if event.Delta.Text != "" {
					return &llm.TextToken{
						Type: enums.TokenTypeText,
						Text: event.Delta.Text,
					}, nil
				}
			case "input_json_delta":
				if event.Delta.PartialJSON != "" && currentToolUse != nil {
					return &llm.ToolCallToken{
						Type:      enums.TokenTypeToolCall,
						ID:        currentToolUse.ID,
						Index:     event.Index,
						Name:      currentToolUse.Name,
						Arguments: event.Delta.PartialJSON,
					}, nil
				}
			}
			return nil, nil

		case "content_block_stop":
			if currentToolUse != nil {
				currentToolUse = nil
			}
			if inThinkingBlock {
				inThinkingBlock = false
			}
			return nil, nil

		case "message_delta":
			var event messageDeltaEvent
			if err := json.Unmarshal(sse.Data, &event); err != nil {
				return nil, errors.Wrap(err, "error unmarshaling message_delta")
			}
			if event.Usage != nil {
				return &llm.UsageToken{
					Type:         enums.TokenTypeUsage,
					InputTokens:  inputTokens,
					OutputTokens: event.Usage.OutputTokens,
					TotalTokens:  inputTokens + event.Usage.OutputTokens,
				}, nil
			}
			return nil, nil

		case "message_stop":
			return &llm.StopToken{
				Type:   enums.TokenTypeStop,
				Reason: "end_turn",
			}, nil

		case "ping":
			return nil, nil

		case "error":
			return &llm.ErrorToken{
				Type: enums.TokenTypeError,
				Err:  errors.Errorf("stream error: %s", string(sse.Data)),
			}, nil

		default:
			// Unknown event type, ignore
			return nil, nil
		}
	}
}
