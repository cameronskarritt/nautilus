package agent

import (
	"context"
	"encoding/json"
	"testing"

	"nautilus/internal/ai/llm"
	"nautilus/internal/enums"
	"nautilus/internal/testutil/require"
)

func TestSkipUnknownTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name         string
		Error        *ToolError
		ExpectedSkip bool
		ExpectedNil  bool
	}{
		{
			Name:         "skips tool not found error",
			Error:        NewToolNotFoundError("unknown_tool"),
			ExpectedSkip: true,
			ExpectedNil:  false,
		},
		{
			Name:         "passes through invalid arguments error",
			Error:        NewInvalidArgumentsError("my_tool", nil),
			ExpectedSkip: false,
			ExpectedNil:  true,
		},
		{
			Name:         "passes through execution error",
			Error:        NewExecutionError("my_tool", nil),
			ExpectedSkip: false,
			ExpectedNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			handler := SkipUnknownTools()
			info := &RepairInfo{
				ToolCall: &llm.ToolCall{ID: "1", Name: "test"},
				Error:    tt.Error,
			}

			repaired, err := handler(context.Background(), info)
			require.NoError(t, err)

			if tt.ExpectedNil {
				require.Nil(t, repaired)
			} else {
				require.NotNil(t, repaired)
				require.Equal(t, tt.ExpectedSkip, repaired.Skip)
			}
		})
	}
}

func TestChainRepair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name         string
		Funcs        []RepairFunc
		ExpectedArgs string
		ExpectedSkip bool
		ExpectedNil  bool
	}{
		{
			Name: "first handler succeeds",
			Funcs: []RepairFunc{
				func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
					return &RepairedCall{Arguments: `{"fixed": true}`}, nil
				},
				func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
					return &RepairedCall{Arguments: `{"second": true}`}, nil
				},
			},
			ExpectedArgs: `{"fixed": true}`,
			ExpectedNil:  false,
		},
		{
			Name: "first returns nil, second succeeds",
			Funcs: []RepairFunc{
				func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
					return nil, nil
				},
				func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
					return &RepairedCall{Arguments: `{"second": true}`}, nil
				},
			},
			ExpectedArgs: `{"second": true}`,
			ExpectedNil:  false,
		},
		{
			Name: "all return nil",
			Funcs: []RepairFunc{
				func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
					return nil, nil
				},
				func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
					return nil, nil
				},
			},
			ExpectedNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			handler := ChainRepair(tt.Funcs...)
			info := &RepairInfo{
				ToolCall: &llm.ToolCall{ID: "1", Name: "test"},
				Error:    NewInvalidArgumentsError("test", nil),
			}

			repaired, err := handler(context.Background(), info)
			require.NoError(t, err)

			if tt.ExpectedNil {
				require.Nil(t, repaired)
			} else {
				require.NotNil(t, repaired)
				require.Equal(t, tt.ExpectedArgs, repaired.Arguments)
			}
		})
	}
}

func TestToolErrorUnwrap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name         string
		Error        *ToolError
		ExpectedKind ToolErrorKind
		ShouldUnwrap error
	}{
		{
			Name:         "tool not found error",
			Error:        NewToolNotFoundError("my_tool"),
			ExpectedKind: ToolErrorKindNotFound,
			ShouldUnwrap: ErrToolNotFound,
		},
		{
			Name:         "invalid arguments error",
			Error:        NewInvalidArgumentsError("my_tool", nil),
			ExpectedKind: ToolErrorKindInvalidArguments,
			ShouldUnwrap: ErrInvalidArguments,
		},
		{
			Name:         "execution error",
			Error:        NewExecutionError("my_tool", nil),
			ExpectedKind: ToolErrorKindExecution,
			ShouldUnwrap: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.ExpectedKind, tt.Error.Kind)
			require.Contains(t, tt.Error.Error(), tt.Error.ToolName)

			if tt.ShouldUnwrap != nil {
				require.ErrorIs(t, tt.Error, tt.ShouldUnwrap)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Input    string
		Expected string
	}{
		{
			Name:     "plain json",
			Input:    `{"key": "value"}`,
			Expected: `{"key": "value"}`,
		},
		{
			Name:     "json in code block",
			Input:    "```json\n{\"key\": \"value\"}\n```",
			Expected: `{"key": "value"}`,
		},
		{
			Name:     "json in plain code block",
			Input:    "```\n{\"key\": \"value\"}\n```",
			Expected: `{"key": "value"}`,
		},
		{
			Name:     "json with surrounding text and code block",
			Input:    "Here's the fixed JSON:\n```json\n{\"fixed\": true}\n```\nDone!",
			Expected: `{"fixed": true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := extractJSON(tt.Input)
			require.Equal(t, tt.Expected, result)
		})
	}
}

// TestRepairToolCalls tests the repair integration in Agent
func TestRepairToolCalls(t *testing.T) {
	t.Parallel()

	// Create a simple tool with schema
	testTool := llm.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters: &llm.Schema{
			Type: llm.TypeObject,
			Properties: llm.Properties{
				"name": llm.String("The name"),
			},
			Required: []string{"name"},
		},
		Call: func(_ context.Context, _ json.RawMessage) (*llm.ToolResult, error) {
			return &llm.ToolResult{Result: "success"}, nil
		},
	}

	tests := []struct {
		Name              string
		Calls             []llm.ToolCall
		RepairHandler     RepairFunc
		MaxAttempts       int
		ExpectedResults   []string
		ExpectedErrorKind []llm.ToolCallErrorKind
	}{
		{
			Name: "skip unknown tool",
			Calls: []llm.ToolCall{
				{
					ID:        "1",
					Name:      "unknown_tool",
					Arguments: "{}",
					Error:     "tool not found",
					ErrorKind: llm.ToolCallErrorNotFound,
				},
			},
			RepairHandler: SkipUnknownTools(),
			MaxAttempts:   1,
			ExpectedResults: []string{
				"", // skipped, empty result
			},
			ExpectedErrorKind: []llm.ToolCallErrorKind{
				llm.ToolCallErrorNone, // error cleared
			},
		},
		{
			Name: "repair invalid arguments",
			Calls: []llm.ToolCall{
				{
					ID:        "1",
					Name:      "test_tool",
					Arguments: "{}",
					Error:     "missing required field",
					ErrorKind: llm.ToolCallErrorInvalidArguments,
				},
			},
			RepairHandler: func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
				return &RepairedCall{Arguments: `{"name": "fixed"}`}, nil
			},
			MaxAttempts: 1,
			ExpectedResults: []string{
				"success",
			},
			ExpectedErrorKind: []llm.ToolCallErrorKind{
				llm.ToolCallErrorNone,
			},
		},
		{
			Name: "max attempts reached",
			Calls: []llm.ToolCall{
				{
					ID:        "1",
					Name:      "test_tool",
					Arguments: "{}",
					Error:     "missing required field",
					ErrorKind: llm.ToolCallErrorInvalidArguments,
				},
			},
			RepairHandler: func(_ context.Context, _ *RepairInfo) (*RepairedCall, error) {
				// Always return bad arguments
				return &RepairedCall{Arguments: `{}`}, nil
			},
			MaxAttempts: 2,
			ExpectedResults: []string{
				`Error: invalid arguments: root: missing required field "name"`, // error result from last attempt
			},
			ExpectedErrorKind: []llm.ToolCallErrorKind{
				llm.ToolCallErrorInvalidArguments, // error persists
			},
		},
		{
			Name: "no repair handler configured",
			Calls: []llm.ToolCall{
				{
					ID:        "1",
					Name:      "unknown_tool",
					Arguments: "{}",
					Error:     "tool not found",
					ErrorKind: llm.ToolCallErrorNotFound,
					Result:    "Error: tool not found",
				},
			},
			RepairHandler: nil,
			MaxAttempts:   1,
			ExpectedResults: []string{
				"Error: tool not found", // unchanged
			},
			ExpectedErrorKind: []llm.ToolCallErrorKind{
				llm.ToolCallErrorNotFound, // unchanged
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			agent := &Agent{
				toolBox:           llm.NewToolbox([]llm.Tool{testTool}),
				messages:          []llm.Message{},
				RepairHandler:     tt.RepairHandler,
				MaxRepairAttempts: tt.MaxAttempts,
			}

			// Make a copy of calls to avoid mutation issues
			calls := make([]llm.ToolCall, len(tt.Calls))
			copy(calls, tt.Calls)

			agent.repairToolCalls(context.Background(), calls)

			require.Len(t, calls, len(tt.ExpectedResults))
			for i, call := range calls {
				require.Equal(t, tt.ExpectedResults[i], call.Result, "result mismatch at index %d", i)
				require.Equal(t, tt.ExpectedErrorKind[i], call.ErrorKind, "error kind mismatch at index %d", i)
			}
		})
	}
}

// mockClient is a simple mock LLM client for testing
type mockClient struct {
	response *llm.Message
	err      error
}

func (m *mockClient) Completion(_ context.Context, _ *llm.Request) (*llm.Message, error) {
	return m.response, m.err
}

func (m *mockClient) StreamCompletion(_ context.Context, _ *llm.Request) (llm.TokenStream, error) {
	return nil, nil
}

func TestReaskRepair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name          string
		Error         *ToolError
		Tool          *llm.Tool
		MockResponse  string
		ExpectedArgs  string
		ExpectedNil   bool
		ExpectedError bool
	}{
		{
			Name:        "skips tool not found",
			Error:       NewToolNotFoundError("unknown"),
			Tool:        nil,
			ExpectedNil: true,
		},
		{
			Name:  "repairs invalid arguments",
			Error: NewInvalidArgumentsError("test_tool", nil),
			Tool: &llm.Tool{
				Name:        "test_tool",
				Description: "A test tool",
				Parameters:  &llm.Schema{Type: llm.TypeObject},
			},
			MockResponse: `{"name": "fixed"}`,
			ExpectedArgs: `{"name": "fixed"}`,
			ExpectedNil:  false,
		},
		{
			Name:  "extracts json from code block",
			Error: NewInvalidArgumentsError("test_tool", nil),
			Tool: &llm.Tool{
				Name:        "test_tool",
				Description: "A test tool",
				Parameters:  &llm.Schema{Type: llm.TypeObject},
			},
			MockResponse: "Here's the fix:\n```json\n{\"name\": \"fixed\"}\n```",
			ExpectedArgs: `{"name": "fixed"}`,
			ExpectedNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			client := &mockClient{
				response: &llm.Message{
					Role:    enums.RoleAssistant,
					Content: tt.MockResponse,
				},
			}

			handler := ReaskRepair(ReaskRepairConfig{
				Client: client,
				Model:  "test-model",
			})

			info := &RepairInfo{
				ToolCall: &llm.ToolCall{ID: "1", Name: "test_tool", Arguments: "{}"},
				Tool:     tt.Tool,
				Error:    tt.Error,
			}

			repaired, err := handler(context.Background(), info)

			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.ExpectedNil {
				require.Nil(t, repaired)
			} else {
				require.NotNil(t, repaired)
				require.Equal(t, tt.ExpectedArgs, repaired.Arguments)
			}
		})
	}
}
