package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"nautilus/internal/ai/llm"
	"nautilus/internal/ai/llm/prompts"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

// Sentinel errors for tool repair
var (
	ErrToolNotFound     = errors.New("tool not found")
	ErrInvalidArguments = errors.New("invalid tool arguments")
	ErrRepairFailed     = errors.New("repair failed")
	ErrMaxAttempts      = errors.New("max repair attempts reached")
)

// ToolErrorKind categorizes tool call errors
type ToolErrorKind int

const (
	ToolErrorKindUnknown ToolErrorKind = iota
	ToolErrorKindNotFound
	ToolErrorKindInvalidArguments
	ToolErrorKindExecution
)

// ToolError wraps tool call errors with context for repair
type ToolError struct {
	Kind     ToolErrorKind
	ToolName string
	Message  string
	Cause    error // underlying validation/execution error
}

func (e *ToolError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("tool %q: %s: %v", e.ToolName, e.Message, e.Cause)
	}
	return fmt.Sprintf("tool %q: %s", e.ToolName, e.Message)
}

func (e *ToolError) Unwrap() error {
	return e.Cause
}

// NewToolNotFoundError creates an error for when a tool doesn't exist
func NewToolNotFoundError(toolName string) *ToolError {
	return &ToolError{
		Kind:     ToolErrorKindNotFound,
		ToolName: toolName,
		Message:  "not found",
		Cause:    ErrToolNotFound,
	}
}

// NewInvalidArgumentsError creates an error for schema validation failures
func NewInvalidArgumentsError(toolName string, cause error) *ToolError {
	return &ToolError{
		Kind:     ToolErrorKindInvalidArguments,
		ToolName: toolName,
		Message:  "invalid arguments",
		Cause:    ErrInvalidArguments,
	}
}

// NewExecutionError creates an error for tool execution failures
func NewExecutionError(toolName string, cause error) *ToolError {
	return &ToolError{
		Kind:     ToolErrorKindExecution,
		ToolName: toolName,
		Message:  "execution failed",
		Cause:    cause,
	}
}

// RepairFunc is called when a tool call fails validation or execution.
// Return a RepairedCall to retry with fixed arguments, or return nil/error to
// pass the error through to the model.
type RepairFunc func(ctx context.Context, info *RepairInfo) (*RepairedCall, error)

// RepairInfo provides context about the failed tool call for repair
type RepairInfo struct {
	ToolCall *llm.ToolCall // the problematic tool call
	Tool     *llm.Tool     // the tool definition (nil if not found)
	Error    *ToolError    // categorized error
	Messages []llm.Message // conversation history for re-ask strategies
	Attempt  int           // repair attempt number (starts at 1)
}

// RepairedCall represents a repaired tool call
type RepairedCall struct {
	Arguments string // fixed JSON arguments to retry with
	Skip      bool   // if true, skip this call entirely (don't send result to model)
}

// SkipUnknownTools returns a repair function that skips hallucinated/unknown tools
// instead of returning an error to the model.
func SkipUnknownTools() RepairFunc {
	return func(_ context.Context, info *RepairInfo) (*RepairedCall, error) {
		if errors.Is(info.Error, ErrToolNotFound) {
			return &RepairedCall{Skip: true}, nil
		}
		// Let other errors pass through
		return nil, nil
	}
}

// ChainRepair combines multiple repair functions, trying each in order until one succeeds.
// Returns the first successful repair, or nil if all return nil/error.
func ChainRepair(funcs ...RepairFunc) RepairFunc {
	return func(ctx context.Context, info *RepairInfo) (*RepairedCall, error) {
		for _, fn := range funcs {
			repaired, err := fn(ctx, info)
			if err != nil {
				continue // try next
			}
			if repaired != nil {
				return repaired, nil
			}
		}
		return nil, nil
	}
}

// ReaskRepairConfig configures the re-ask repair strategy
type ReaskRepairConfig struct {
	Client llm.Client
	Model  enums.Model
}

// repairPromptData holds the data for the repair prompt template
type repairPromptData struct {
	ToolName          string
	ToolDescription   string
	ExpectedSchema    string
	Error             string
	OriginalArguments string
}

// ReaskRepair creates a repair function that asks the model to fix its arguments.
// It sends the error back to the model with the tool schema and asks for corrected arguments.
func ReaskRepair(cfg ReaskRepairConfig) RepairFunc {
	return func(ctx context.Context, info *RepairInfo) (*RepairedCall, error) {
		// Can't re-ask for unknown tools
		if errors.Is(info.Error, ErrToolNotFound) {
			return nil, nil
		}

		if info.Tool == nil {
			return nil, nil
		}

		data := repairPromptData{
			ToolName:          info.Tool.Name,
			ToolDescription:   info.Tool.Description,
			ExpectedSchema:    schemaToString(info.Tool.Parameters),
			Error:             info.Error.Error(),
			OriginalArguments: info.ToolCall.Arguments,
		}
		systemPrompt, err := prompts.Execute("repair", data)
		if err != nil {
			return nil, errors.Wrap(err, "failed to execute repair prompt template")
		}

		messages := []llm.Message{
			{
				Role:    enums.RoleUser,
				Content: systemPrompt,
			},
		}

		response, err := cfg.Client.Completion(ctx, &llm.Request{
			Model:    cfg.Model,
			Messages: messages,
		})
		if err != nil {
			return nil, errors.Wrap(err, ErrRepairFailed.Error()+": failed to get repair response")
		}

		if response.Content == "" {
			return nil, errors.Errorf("%s: empty repair response", ErrRepairFailed)
		}

		fixedArgs := extractJSON(response.Content)
		return &RepairedCall{Arguments: fixedArgs}, nil
	}
}

func schemaToString(schema *llm.Schema) string {
	if schema == nil {
		return "{}"
	}
	// Simple JSON marshaling for now
	data, err := json.Marshal(schema)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// extractJSON attempts to extract JSON from a response that might be wrapped in markdown
func extractJSON(content string) string {
	content = strings.TrimSpace(content)

	// Try to find JSON in code blocks
	if strings.Contains(content, "```") {
		// Look for ```json or just ```
		start := strings.Index(content, "```")
		if start != -1 {
			// Skip the opening ``` and optional language tag
			rest := content[start+3:]
			if idx := strings.Index(rest, "\n"); idx != -1 {
				rest = rest[idx+1:]
			}
			// Find closing ```
			if end := strings.Index(rest, "```"); end != -1 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}

	// Return as-is if no code blocks
	return content
}
