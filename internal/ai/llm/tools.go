package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

type Tool struct {
	Name             string
	Description      string
	Parameters       *Schema
	Timeout          time.Duration // Optional per-tool timeout
	RequiresApproval bool
	Call             func(ctx context.Context, args json.RawMessage) (*ToolResult, error)
}

type ToolResult struct {
	Result string `json:"result"`
	Tools  []Tool `json:"tools"`
}

// ToolCallErrorKind categorizes tool call errors for repair handling
type ToolCallErrorKind int

const (
	ToolCallErrorNone ToolCallErrorKind = iota
	ToolCallErrorNotFound
	ToolCallErrorInvalidArguments
	ToolCallErrorExecution
)

type ToolCall struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	Result    string            `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
	ErrorKind ToolCallErrorKind `json:"-"` // not serialized, used for repair logic
}

func (t *ToolCall) Message() Message {
	return Message{
		Role:      enums.RoleTool,
		ToolCalls: []ToolCall{*t},
	}
}

type toolBuffer struct {
	ID   string
	Name string
	buf  *bytes.Buffer
}

type Toolbox struct {
	mu      sync.Mutex
	tools   map[string]Tool
	buffers map[int]*toolBuffer
}

type ToolboxResult struct {
	Calls []ToolCall
	tools []Tool
}

func (t *ToolboxResult) Tools() []Tool {
	return t.tools
}

func NewToolboxResult(calls []ToolCall, tools []Tool) *ToolboxResult {
	return &ToolboxResult{
		Calls: calls,
		tools: tools,
	}
}

func NewToolbox(tools []Tool) *Toolbox {
	tb := &Toolbox{
		tools:   make(map[string]Tool),
		buffers: make(map[int]*toolBuffer),
	}

	tb.Add(tools)
	return tb
}

func (t *Toolbox) AddToolResult(result *ToolboxResult) {
	t.Add(result.Tools())
}

func (t *Toolbox) Add(tools []Tool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tools == nil {
		t.tools = make(map[string]Tool)
	}

	for _, tool := range tools {
		t.tools[tool.Name] = tool
	}
}

func (t *Toolbox) HandleToken(token *ToolCallToken) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tb, ok := t.buffers[token.Index]
	if !ok {
		tb = &toolBuffer{
			ID:   token.ID,
			Name: token.Name,
			buf:  bytes.NewBuffer(nil),
		}
		t.buffers[token.Index] = tb
	}
	_, err := tb.buf.WriteString(token.Arguments)
	if err != nil {
		return errors.Wrap(err, "error writing to tool buffer")
	}

	return nil
}

func (t *Toolbox) clearBuffers() {
	t.buffers = make(map[int]*toolBuffer)
}

// ClearBuffers discards all buffered tool calls without executing them.
func (t *Toolbox) ClearBuffers() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearBuffers()
}

// HasApprovalRequired returns true if any buffered tool call references a tool
// marked with RequiresApproval. Used to gate execution before Flush.
func (t *Toolbox) HasApprovalRequired() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, tb := range t.buffers {
		if tool, ok := t.tools[tb.Name]; ok && tool.RequiresApproval {
			return true
		}
	}
	return false
}

// PendingCalls extracts buffered tool calls as ToolCall values without executing them.
func (t *Toolbox) PendingCalls() []ToolCall {
	t.mu.Lock()
	defer t.mu.Unlock()

	calls := make([]ToolCall, 0, len(t.buffers))
	for _, tb := range t.buffers {
		calls = append(calls, ToolCall{
			ID:        tb.ID,
			Name:      tb.Name,
			Arguments: tb.buf.String(),
		})
	}
	return calls
}

func (t *Toolbox) Tools() []Tool {
	t.mu.Lock()
	defer t.mu.Unlock()

	tools := make([]Tool, 0, len(t.tools))
	for _, tool := range t.tools {
		tools = append(tools, tool)
	}
	return tools
}

// GetTool returns a tool by name, or nil if not found
func (t *Toolbox) GetTool(name string) *Tool {
	t.mu.Lock()
	defer t.mu.Unlock()

	tool, ok := t.tools[name]
	if !ok {
		return nil
	}
	return &tool
}

// ExecuteCall executes a single tool call with the given arguments.
// This is used by repair handlers to retry with fixed arguments.
func (t *Toolbox) ExecuteCall(ctx context.Context, call *ToolCall, args string) (*ToolCall, []Tool) {
	t.mu.Lock()
	tool, ok := t.tools[call.Name]
	t.mu.Unlock()

	result := &ToolCall{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: args,
	}

	if !ok {
		result.Error = fmt.Sprintf("tool not found: %s", call.Name)
		result.ErrorKind = ToolCallErrorNotFound
		result.Result = fmt.Sprintf("Error: tool %q does not exist", call.Name)
		return result, nil
	}

	if tool.Parameters != nil {
		if err := tool.Parameters.Validate([]byte(args)); err != nil {
			result.Error = err.Error()
			result.ErrorKind = ToolCallErrorInvalidArguments
			result.Result = fmt.Sprintf("Error: invalid arguments: %s", err.Error())
			return result, nil
		}
	}

	callCtx := ctx
	if tool.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, tool.Timeout)
		defer cancel()
	}

	toolResult, err := tool.Call(callCtx, []byte(args))
	if err != nil {
		result.Error = err.Error()
		result.ErrorKind = ToolCallErrorExecution
		result.Result = fmt.Sprintf("Error: %s", err.Error())
		return result, nil
	}

	result.Result = toolResult.Result
	return result, toolResult.Tools
}

func (t *Toolbox) Flush(ctx context.Context) (*ToolboxResult, error) {
	t.mu.Lock()
	buffers := make([]*toolBuffer, 0, len(t.buffers))
	for _, tb := range t.buffers {
		buffers = append(buffers, tb)
	}
	t.mu.Unlock()

	if len(buffers) == 0 {
		t.mu.Lock()
		t.clearBuffers()
		t.mu.Unlock()
		return &ToolboxResult{
			Calls: make([]ToolCall, 0),
			tools: make([]Tool, 0),
		}, nil
	}

	type callResult struct {
		call  ToolCall
		tools []Tool
	}

	results := make([]callResult, len(buffers))
	var wg sync.WaitGroup
	wg.Add(len(buffers))

	for i, tb := range buffers {
		go func(i int, tb *toolBuffer) {
			defer wg.Done()

			t.mu.Lock()
			tool, ok := t.tools[tb.Name]
			t.mu.Unlock()

			if !ok {
				results[i] = callResult{
					call: ToolCall{
						ID:        tb.ID,
						Name:      tb.Name,
						Arguments: tb.buf.String(),
						Error:     fmt.Sprintf("tool not found: %s", tb.Name),
						ErrorKind: ToolCallErrorNotFound,
						Result:    fmt.Sprintf("Error: tool %q does not exist", tb.Name),
					},
				}
				return
			}

			if tool.Parameters != nil {
				if err := tool.Parameters.Validate(tb.buf.Bytes()); err != nil {
					results[i] = callResult{
						call: ToolCall{
							ID:        tb.ID,
							Name:      tb.Name,
							Arguments: tb.buf.String(),
							Error:     err.Error(),
							ErrorKind: ToolCallErrorInvalidArguments,
							Result:    fmt.Sprintf("Error: invalid arguments: %s", err.Error()),
						},
					}
					return
				}
			}

			callCtx := ctx
			if tool.Timeout > 0 {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithTimeout(ctx, tool.Timeout)
				defer cancel()
			}

			result, err := tool.Call(callCtx, tb.buf.Bytes())
			if err != nil {
				results[i] = callResult{
					call: ToolCall{
						ID:        tb.ID,
						Name:      tb.Name,
						Arguments: tb.buf.String(),
						Error:     err.Error(),
						ErrorKind: ToolCallErrorExecution,
						Result:    fmt.Sprintf("Error: %s", err.Error()),
					},
				}
				return
			}

			results[i] = callResult{
				call: ToolCall{
					ID:        tb.ID,
					Name:      tb.Name,
					Arguments: tb.buf.String(),
					Result:    result.Result,
				},
				tools: result.Tools,
			}
		}(i, tb)
	}

	wg.Wait()

	toolboxResult := &ToolboxResult{
		Calls: make([]ToolCall, 0, len(results)),
		tools: make([]Tool, 0),
	}

	for _, r := range results {
		toolboxResult.Calls = append(toolboxResult.Calls, r.call)
		toolboxResult.tools = append(toolboxResult.tools, r.tools...)
	}

	t.mu.Lock()
	t.clearBuffers()
	t.mu.Unlock()

	return toolboxResult, nil
}
