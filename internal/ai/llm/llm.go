package llm

import (
	"context"
	"encoding/json"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

type Client interface {
	StreamCompletion(ctx context.Context, request *Request) (TokenStream, error)
	Completion(ctx context.Context, request *Request) (*Message, error)
}

type Request struct {
	Model            enums.Model       `json:"model"`
	Messages         []Message         `json:"messages"`
	Tools            []Tool            `json:"tools,omitempty"`
	ToolChoice       *ToolChoice       `json:"tool_choice,omitempty"`
	StructuredOutput *StructuredOutput `json:"structured_output,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type Message struct {
	Role        enums.Role   `json:"role"`
	Reasoning   string       `json:"reasoning,omitempty"`
	Content     string       `json:"content,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Usage       *Usage       `json:"usage,omitempty"`
	Metrics     *Metrics     `json:"metrics,omitempty"`
}

type Attachment struct {
	URL       string `json:"url,omitempty"`
	Base64    string `json:"base64,omitempty"`
	MediaType string `json:"media_type,omitempty"` // e.g., "image/jpeg", "image/png"
	Detail    string `json:"detail,omitempty"`     // OpenAI detail level: "low", "high", "auto"
}

type ToolChoice struct {
	Mode string // "none", "auto", "required", "function"
	Name string // only used when Mode is "function"
}

func (tc *ToolChoice) MarshalJSON() ([]byte, error) {
	if tc == nil {
		return []byte("null"), nil
	}

	if tc.Name != "" || tc.Mode == "function" {
		buf, err := json.Marshal(map[string]any{
			"type": "function",
			"function": map[string]string{
				"name": tc.Name,
			},
		})
		if err != nil {
			return nil, errors.Wrap(err, "error marshalling ToolChoice")
		}
		return buf, nil
	}

	buf, err := json.Marshal(tc.Mode)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling ToolChoice")
	}
	return buf, nil
}

func ToolChoiceNone() *ToolChoice {
	return &ToolChoice{Mode: "none"}
}

func ToolChoiceAuto() *ToolChoice {
	return &ToolChoice{Mode: "auto"}
}

func ToolChoiceRequired() *ToolChoice {
	return &ToolChoice{Mode: "required"}
}

func ToolChoiceFunction(name string) *ToolChoice {
	return &ToolChoice{Mode: "function", Name: name}
}

type request struct {
	Model            enums.Model       `json:"model"`
	Messages         []Message         `json:"messages"`
	Tools            []tool            `json:"tools,omitempty"`
	ToolChoice       *ToolChoice       `json:"tool_choice,omitempty"`
	StructuredOutput *StructuredOutput `json:"structured_output,omitempty"`
}

type tool struct {
	Name        string  `json:"name,omitempty"`
	Description string  `json:"description,omitempty"`
	Parameters  *Schema `json:"parameters,omitempty"`
}

func (r *Request) MarshalJSON() ([]byte, error) {
	aux := request{
		Model:            r.Model,
		Messages:         r.Messages,
		ToolChoice:       r.ToolChoice,
		StructuredOutput: r.StructuredOutput,
	}
	tools := make([]tool, len(r.Tools))
	for i, t := range r.Tools {
		tools[i] = tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	aux.Tools = tools

	buf, err := json.Marshal(aux)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling Request")
	}
	return buf, nil
}
