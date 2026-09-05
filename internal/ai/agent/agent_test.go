package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/llm"
	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

// ---------------------------------------------------------------------------
// Test infrastructure: MemoryEventLog
// ---------------------------------------------------------------------------

// memoryEventLog is an in-memory EventLog that stores events with proper
// sequence numbers and supports fence token validation.
type memoryEventLog struct {
	mu          sync.Mutex
	events      map[string][]*eventlog.Event // streamID -> events
	fenceTokens map[string]int64             // streamID -> current fence token
	seq         int64
}

func newMemoryEventLog() *memoryEventLog {
	return &memoryEventLog{
		events:      make(map[string][]*eventlog.Event),
		fenceTokens: make(map[string]int64),
	}
}

// SetFenceToken sets the expected fence token for a stream.
func (m *memoryEventLog) SetFenceToken(streamID string, token int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fenceTokens[streamID] = token
}

func (m *memoryEventLog) Append(_ context.Context, streamID string, eventType enums.AgentEventType, source enums.AgentEventSource, payload any, tokens eventlog.Tokens) (*eventlog.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tokens.Idempotency != "" {
		for _, event := range m.events[streamID] {
			if event.Type == eventType && event.IdempotencyKey != nil && *event.IdempotencyKey == tokens.Idempotency {
				return event, nil
			}
		}
	}

	// Check fence token if one has been set
	if expected, ok := m.fenceTokens[streamID]; ok {
		if tokens.Fence != expected {
			return nil, eventlog.ErrFenceViolation
		}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal event payload")
	}

	m.seq++

	var idempotencyKey *string
	if tokens.Idempotency != "" {
		idempotencyKey = &tokens.Idempotency
	}

	event := &eventlog.Event{
		ID:             "evt-" + streamID + "-" + eventType.String(),
		StreamID:       streamID,
		Sequence:       m.seq,
		Type:           eventType,
		Source:         source,
		IdempotencyKey: idempotencyKey,
		Payload:        payloadJSON,
		CreatedAt:      time.Now(),
	}

	m.events[streamID] = append(m.events[streamID], event)
	return event, nil
}

func (m *memoryEventLog) List(_ context.Context, streamID string, afterSequence int64) ([]*eventlog.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all := m.events[streamID]
	var result []*eventlog.Event
	for _, e := range all {
		if e.Sequence > afterSequence {
			result = append(result, e)
		}
	}
	return result, nil
}

// EventTypes returns the types of all events for a stream (for assertion).
func (m *memoryEventLog) EventTypes(streamID string) []enums.AgentEventType {
	m.mu.Lock()
	defer m.mu.Unlock()

	var types []enums.AgentEventType
	for _, e := range m.events[streamID] {
		types = append(types, e.Type)
	}
	return types
}

// Seed adds a pre-existing event to the log (for replay testing).
func (m *memoryEventLog) Seed(streamID string, eventType enums.AgentEventType, payload any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		panic(errors.Wrap(err, "unable to marshal seeded payload"))
	}
	m.seq++

	event := &eventlog.Event{
		ID:        "seed",
		StreamID:  streamID,
		Sequence:  m.seq,
		Type:      eventType,
		Payload:   payloadJSON,
		CreatedAt: time.Now(),
	}

	m.events[streamID] = append(m.events[streamID], event)
}

var _ eventlog.EventLog = (*memoryEventLog)(nil)

// ---------------------------------------------------------------------------
// Test infrastructure: mock LLM client and token stream
// ---------------------------------------------------------------------------

type streamingMockClient struct {
	mu        sync.Mutex
	calls     int
	responses [][]llm.Token // one slice of tokens per call
}

func newStreamingMockClient(responses ...[]llm.Token) *streamingMockClient {
	return &streamingMockClient{responses: responses}
}

func (c *streamingMockClient) StreamCompletion(_ context.Context, _ *llm.Request) (llm.TokenStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	idx := c.calls
	if idx >= len(c.responses) {
		idx = len(c.responses) - 1
	}
	c.calls++

	tokens := c.responses[idx]
	ch := make(chan llm.Token, len(tokens))
	for _, t := range tokens {
		ch <- t
	}
	close(ch)

	_, cancel := context.WithCancel(context.Background())
	return llm.NewTokenStream(ch, cancel), nil
}

func (c *streamingMockClient) Completion(_ context.Context, _ *llm.Request) (*llm.Message, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Test infrastructure: mock publisher
// ---------------------------------------------------------------------------

type mockPublisher struct {
	mu       sync.Mutex
	messages []publishedMessage
}

type publishedMessage struct {
	Topic string
	Data  any
}

func newMockPublisher() *mockPublisher {
	return &mockPublisher{messages: make([]publishedMessage, 0)}
}

func (p *mockPublisher) Publish(_ context.Context, topic string, message any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, publishedMessage{Topic: topic, Data: message})
	return nil
}

func (p *mockPublisher) Published() []publishedMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	msgs := make([]publishedMessage, len(p.messages))
	copy(msgs, p.messages)
	return msgs
}

func findApprovalRequestedPublication(messages []publishedMessage) (ApprovalRequestedEventPayload, bool) {
	for _, msg := range messages {
		data, ok := msg.Data.(ApprovalRequestedEventPayload)
		if !ok {
			continue
		}
		if data.Type == "approval_requested" {
			return data, true
		}
	}
	return ApprovalRequestedEventPayload{}, false
}

func findApprovalResolvedPublication(messages []publishedMessage) (ApprovalResolvedEventPayload, bool) {
	for _, msg := range messages {
		data, ok := msg.Data.(ApprovalResolvedEventPayload)
		if !ok {
			continue
		}
		if data.Type == "approval_resolved" {
			return data, true
		}
	}
	return ApprovalResolvedEventPayload{}, false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func simpleTextTokens(text string) []llm.Token {
	return []llm.Token{
		&llm.TextToken{Type: enums.TokenTypeText, Text: text},
		&llm.UsageToken{Type: enums.TokenTypeUsage, InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

func newTestAgent(client llm.Client, el eventlog.EventLog, pub *mockPublisher) *Agent {
	clients := llm.NewClientRegistry()
	clients.Register("test-model", client)
	return NewAgent("stream-1", 1, 1, nil, clients, nil, el, pub).WithDefaultModel("test-model")
}

func approvalToolCallTokens() []llm.Token {
	return []llm.Token{
		&llm.ToolCallToken{Type: enums.TokenTypeToolCall, ID: "call-1", Index: 0, Name: "gated_tool", Arguments: "{}"},
		&llm.UsageToken{Type: enums.TokenTypeUsage, InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

var approvalRequiredTool = llm.Tool{
	Name:             "gated_tool",
	Description:      "needs approval",
	RequiresApproval: true,
	Parameters:       &llm.Schema{Type: llm.TypeObject, Properties: llm.Properties{}},
	Call: func(_ context.Context, _ json.RawMessage) (*llm.ToolResult, error) {
		return &llm.ToolResult{Result: "gated tool executed"}, nil
	},
}

// ---------------------------------------------------------------------------
// Tests: HandleEvent
// ---------------------------------------------------------------------------

func TestAgent_HandleEvent_dispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType enums.AgentEventType
		payload   json.RawMessage
		setup     func(el *memoryEventLog)
		wantErr   bool
		errIs     error
		errSubstr string
	}{
		{
			name:      "fence violation",
			eventType: enums.AgentEventSignalReceived,
			payload:   json.RawMessage(`{"content":"hi"}`),
			setup: func(el *memoryEventLog) {
				el.SetFenceToken("stream-1", 999)
			},
			wantErr: true,
			errIs:   eventlog.ErrFenceViolation,
		},
		{
			name:      "stop",
			eventType: enums.AgentEventSignalStop,
			wantErr:   false,
		},
		{
			name:      "unknown type",
			eventType: enums.AgentEventType("bogus"),
			wantErr:   true,
			errSubstr: "unknown event type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			el := newMemoryEventLog()
			el.SetFenceToken("stream-1", 1)
			if tt.setup != nil {
				tt.setup(el)
			}

			client := newStreamingMockClient(simpleTextTokens("hello"))
			pub := newMockPublisher()
			a := newTestAgent(client, el, pub)

			_, err := a.HandleEvent(context.Background(), tt.eventType, enums.AgentEventSourceAPI, tt.payload)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
			}
			if tt.errSubstr != "" {
				require.ErrorContains(t, err, tt.errSubstr)
			}
		})
	}
}

func TestAgent_HandleEvent_new_message(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	el.SetFenceToken("stream-1", 1)

	client := newStreamingMockClient(simpleTextTokens("Hello!"))
	pub := newMockPublisher()
	a := newTestAgent(client, el, pub)

	_, err := a.HandleEvent(context.Background(), enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"hi"}`))
	require.NoError(t, err)

	types := el.EventTypes("stream-1")
	require.Contains(t, types, enums.AgentEventSignalReceived)
	require.Contains(t, types, enums.AgentEventMessage)
	require.Contains(t, types, enums.AgentEventTurnStarted)
	require.Contains(t, types, enums.AgentEventLLMResponse)
	require.Contains(t, types, enums.AgentEventTurnCompleted)

	// Individual token events (llm.text, llm.reasoning) should NOT be in
	// the event log — they go through PubSub only.
	for _, typ := range types {
		if typ == enums.AgentEventLLMText || typ == enums.AgentEventLLMReasoning {
			t.Errorf("unexpected token-level event in event log: %s", typ)
		}
	}

	msgs := a.Messages()
	require.Len(t, msgs, 2)
	require.Equal(t, enums.RoleUser, msgs[0].Role)
	require.Equal(t, "hi", msgs[0].Content)
	require.Equal(t, enums.RoleAssistant, msgs[1].Role)
	require.Equal(t, "Hello!", msgs[1].Content)
}

func TestAgent_HandleEvent_prePersistedMessage(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	el.SetFenceToken("stream-1", 1)

	ctx := context.Background()
	msg := MessagePayload{ID: "msg-1", Content: "hi"}
	tokens := eventlog.Tokens{Fence: 1, Idempotency: "msg-1"}
	_, err := el.Append(ctx, "stream-1", enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, nil, tokens)
	require.NoError(t, err)
	_, err = el.Append(ctx, "stream-1", enums.AgentEventMessage, enums.AgentEventSourceAPI, msg, tokens)
	require.NoError(t, err)

	client := newStreamingMockClient(simpleTextTokens("Hello!"))
	pub := newMockPublisher()
	a := newTestAgent(client, el, pub)
	_, err = a.ReplayFromLog(ctx)
	require.NoError(t, err)

	_, err = a.HandleEvent(ctx, enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"id":"msg-1","content":"hi"}`))
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 2)
	require.Equal(t, enums.RoleUser, msgs[0].Role)
	require.Equal(t, "hi", msgs[0].Content)
	require.Equal(t, enums.RoleAssistant, msgs[1].Role)
	require.Equal(t, "Hello!", msgs[1].Content)

	var turnStarted int
	for _, typ := range el.EventTypes("stream-1") {
		if typ == enums.AgentEventTurnStarted {
			turnStarted++
		}
	}
	require.Equal(t, 1, turnStarted)
}

// ---------------------------------------------------------------------------
// Tests: PubSub
// ---------------------------------------------------------------------------

func TestAgent_publishToken(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	el.SetFenceToken("stream-1", 1)

	client := newStreamingMockClient(simpleTextTokens("hi"))
	pub := newMockPublisher()
	a := newTestAgent(client, el, pub)

	_, err := a.HandleEvent(context.Background(), enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"hello"}`))
	require.NoError(t, err)

	published := pub.Published()
	require.True(t, len(published) > 0)
	require.Equal(t, "agent:stream-1", published[0].Topic)

	token, ok := published[0].Data.(TokenEventPayload)
	require.True(t, ok)
	require.Equal(t, enums.TokenTypeText, token.Type)
	require.Equal(t, "hi", token.Content)
	require.Nil(t, token.Index)
}

// ---------------------------------------------------------------------------
// Tests: ReplayFromLog
// ---------------------------------------------------------------------------

func TestAgent_ReplayFromLog_empty_stream(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()
	a := newTestAgent(nil, el, pub)

	_, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)
	require.Len(t, a.Messages(), 0)
}

func TestAgent_ReplayFromLog_single_turn(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()

	// Seed events for a single turn
	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "hello"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)
	el.Seed("stream-1", enums.AgentEventLLMText, MessagePayload{Content: "Hi there!"})
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role:    enums.RoleAssistant,
		Content: "Hi there!",
	})
	el.Seed("stream-1", enums.AgentEventTurnCompleted, nil)

	a := newTestAgent(nil, el, pub)

	_, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 2)

	require.Equal(t, enums.RoleUser, msgs[0].Role)
	require.Equal(t, "hello", msgs[0].Content)

	require.Equal(t, enums.RoleAssistant, msgs[1].Role)
	require.Equal(t, "Hi there!", msgs[1].Content)
}

func TestAgent_ReplayFromLog_multi_turn(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()

	// Turn 1
	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "hello"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role:    enums.RoleAssistant,
		Content: "Hi! How can I help?",
	})
	el.Seed("stream-1", enums.AgentEventTurnCompleted, nil)

	// Turn 2
	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "what is 2+2?"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role:    enums.RoleAssistant,
		Content: "2+2 is 4.",
	})
	el.Seed("stream-1", enums.AgentEventTurnCompleted, nil)

	a := newTestAgent(nil, el, pub)

	_, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 4)

	require.Equal(t, enums.RoleUser, msgs[0].Role)
	require.Equal(t, "hello", msgs[0].Content)
	require.Equal(t, enums.RoleAssistant, msgs[1].Role)
	require.Equal(t, "Hi! How can I help?", msgs[1].Content)
	require.Equal(t, enums.RoleUser, msgs[2].Role)
	require.Equal(t, "what is 2+2?", msgs[2].Content)
	require.Equal(t, enums.RoleAssistant, msgs[3].Role)
	require.Equal(t, "2+2 is 4.", msgs[3].Content)
}

func TestAgent_ReplayFromLog_with_tool_calls(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()

	// User message
	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "what is the weather?"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)

	// Assistant response with tool call
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role: enums.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "get_weather", Arguments: `{"city":"NYC"}`, Result: "Sunny, 72F"},
		},
	})

	// Tool call event (logged but not used for replay)
	el.Seed("stream-1", enums.AgentEventToolCall, []llm.ToolCall{
		{ID: "call-1", Name: "get_weather", Arguments: `{"city":"NYC"}`, Result: "Sunny, 72F"},
	})

	// Tool result message
	el.Seed("stream-1", enums.AgentEventToolResult, llm.Message{
		Role: enums.RoleTool,
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "get_weather", Arguments: `{"city":"NYC"}`, Result: "Sunny, 72F"},
		},
	})

	// Second LLM response (final)
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role:    enums.RoleAssistant,
		Content: "The weather in NYC is sunny and 72F.",
	})
	el.Seed("stream-1", enums.AgentEventTurnCompleted, nil)

	a := newTestAgent(nil, el, pub)

	_, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 4)

	// User message
	require.Equal(t, enums.RoleUser, msgs[0].Role)
	require.Equal(t, "what is the weather?", msgs[0].Content)

	// Assistant with tool call
	require.Equal(t, enums.RoleAssistant, msgs[1].Role)
	require.Len(t, msgs[1].ToolCalls, 1)
	require.Equal(t, "get_weather", msgs[1].ToolCalls[0].Name)

	// Tool result
	require.Equal(t, enums.RoleTool, msgs[2].Role)

	// Final assistant response
	require.Equal(t, enums.RoleAssistant, msgs[3].Role)
	require.Equal(t, "The weather in NYC is sunny and 72F.", msgs[3].Content)
}

func TestAgent_ReplayFromLog_then_continue(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	el.SetFenceToken("stream-1", 1)
	pub := newMockPublisher()

	// Seed a previous turn
	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "hello"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role:    enums.RoleAssistant,
		Content: "Hi! How can I help?",
	})
	el.Seed("stream-1", enums.AgentEventTurnCompleted, nil)

	client := newStreamingMockClient(simpleTextTokens("2+2 is 4."))
	a := newTestAgent(client, el, pub)

	_, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)

	msgs := a.Messages()
	require.Len(t, msgs, 2)

	// Send a new message via HandleEvent
	_, err = a.HandleEvent(context.Background(), enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"what is 2+2?"}`))
	require.NoError(t, err)

	msgs = a.Messages()
	require.Len(t, msgs, 4)

	require.Equal(t, "hello", msgs[0].Content)
	require.Equal(t, "Hi! How can I help?", msgs[1].Content)
	require.Equal(t, "what is 2+2?", msgs[2].Content)
	require.Equal(t, "2+2 is 4.", msgs[3].Content)
}

func TestAgent_Replay_equivalence(t *testing.T) {
	t.Parallel()

	// --- Phase 1: Process a turn via HandleEvent ---
	el := newMemoryEventLog()
	el.SetFenceToken("stream-1", 1)
	pub := newMockPublisher()

	client := newStreamingMockClient(simpleTextTokens("The answer is 42."))
	agentA := newTestAgent(client, el, pub)

	_, err := agentA.HandleEvent(context.Background(), enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"what is the meaning of life?"}`))
	require.NoError(t, err)

	originalMessages := agentA.Messages()
	require.Len(t, originalMessages, 2)

	// --- Phase 2: Replay from the same event log ---
	pub2 := newMockPublisher()
	agentB := newTestAgent(nil, el, pub2)

	_, err = agentB.ReplayFromLog(context.Background())
	require.NoError(t, err)

	replayedMessages := agentB.Messages()

	// --- Phase 3: Assert equivalence ---
	require.Len(t, replayedMessages, len(originalMessages))

	for i := range originalMessages {
		require.Equal(t, originalMessages[i].Role, replayedMessages[i].Role, "role mismatch at index %d", i)
		require.Equal(t, originalMessages[i].Content, replayedMessages[i].Content, "content mismatch at index %d", i)
		require.Len(t, replayedMessages[i].ToolCalls, len(originalMessages[i].ToolCalls), "tool calls length mismatch at index %d", i)
	}
}

// ---------------------------------------------------------------------------
// Tests: Approval flow
// ---------------------------------------------------------------------------

func TestAgent_HandleEvent_approval_gating(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	fenceToken, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	el := newMemoryEventLog()
	el.SetFenceToken(stream.ExternalID, fenceToken)
	pub := newMockPublisher()

	client := newStreamingMockClient(approvalToolCallTokens())
	clients := llm.NewClientRegistry()
	clients.Register("test-model", client)

	a := NewAgent(stream.ExternalID, stream.ID, fenceToken, db, clients, []llm.Tool{approvalRequiredTool}, el, pub).
		WithDefaultModel("test-model")

	pending, err := a.HandleEvent(ctx, enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"do the gated thing"}`))
	require.NoError(t, err)

	require.NotNil(t, pending)

	types := el.EventTypes(stream.ExternalID)
	require.Contains(t, types, enums.AgentEventApprovalRequested)

	published := pub.Published()
	data, ok := findApprovalRequestedPublication(published)
	require.True(t, ok, "expected approval_requested PubSub message")
	require.Equal(t, "approval_requested", data.Type)
	require.Len(t, data.ToolCalls, 1)
	require.Equal(t, "gated_tool", data.ToolCalls[0].Name)

	msgs := a.Messages()
	require.Len(t, msgs, 2)
	require.Equal(t, enums.RoleUser, msgs[0].Role)
	require.Equal(t, enums.RoleAssistant, msgs[1].Role)
	require.Len(t, msgs[1].ToolCalls, 1)
	require.Equal(t, "gated_tool", msgs[1].ToolCalls[0].Name)

	approval, err := agentapprovals.GetPendingByStreamID(ctx, db, stream.ID)
	require.NoError(t, err)
	require.NotNil(t, approval)
	require.Equal(t, agentapprovals.StatusPending, approval.Status)
}

func TestAgent_HandleEvent_approval_approved(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	fenceToken, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	el := newMemoryEventLog()
	el.SetFenceToken(stream.ExternalID, fenceToken)
	pub := newMockPublisher()

	client := newStreamingMockClient(
		approvalToolCallTokens(),
		simpleTextTokens("done after approval"),
	)
	clients := llm.NewClientRegistry()
	clients.Register("test-model", client)

	a := NewAgent(stream.ExternalID, stream.ID, fenceToken, db, clients, []llm.Tool{approvalRequiredTool}, el, pub).
		WithDefaultModel("test-model")

	pending, err := a.HandleEvent(ctx, enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"do it"}`))
	require.NoError(t, err)
	require.NotNil(t, pending)

	approvalID := pending.ApprovalID

	decision, err := json.Marshal(ApprovalDecision{
		ApprovalID: approvalID,
		Approved:   true,
		Reason:     "include this context",
	})
	require.NoError(t, err)
	pending, err = a.HandleEvent(ctx, enums.AgentEventSignalApproval, enums.AgentEventSourceAPI, decision)
	require.NoError(t, err)
	require.Nil(t, pending)

	types := el.EventTypes(stream.ExternalID)
	require.Contains(t, types, enums.AgentEventApprovalResolved)

	events, err := el.List(ctx, stream.ExternalID, 0)
	require.NoError(t, err)
	var foundInternalComment bool
	for _, event := range events {
		if event.Type != enums.AgentEventMessage || event.Source != enums.AgentEventSourceInternal {
			continue
		}

		var msg MessagePayload
		require.NoError(t, json.Unmarshal(event.Payload, &msg))
		require.Equal(t, "[Approval comment from user]: include this context", msg.Content)
		foundInternalComment = true
	}
	require.True(t, foundInternalComment)

	published := pub.Published()
	data, ok := findApprovalResolvedPublication(published)
	require.True(t, ok, "expected approval_resolved PubSub message")
	require.Equal(t, "approval_resolved", data.Type)
	require.True(t, data.Approved)
	require.Equal(t, approvalID, data.ApprovalID)

	msgs := a.Messages()
	var toolExecuted bool
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Name == "gated_tool" && tc.Result == "gated tool executed" {
				toolExecuted = true
			}
		}
	}
	require.True(t, toolExecuted, "expected gated tool to be executed")

	last := msgs[len(msgs)-1]
	require.Equal(t, enums.RoleAssistant, last.Role)
	require.Equal(t, "done after approval", last.Content)
}

func TestAgent_HandleEvent_approval_rejected(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	fenceToken, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	el := newMemoryEventLog()
	el.SetFenceToken(stream.ExternalID, fenceToken)
	pub := newMockPublisher()

	client := newStreamingMockClient(
		approvalToolCallTokens(),
		simpleTextTokens("understood, not doing it"),
	)
	clients := llm.NewClientRegistry()
	clients.Register("test-model", client)

	a := NewAgent(stream.ExternalID, stream.ID, fenceToken, db, clients, []llm.Tool{approvalRequiredTool}, el, pub).
		WithDefaultModel("test-model")

	pending, err := a.HandleEvent(ctx, enums.AgentEventSignalReceived, enums.AgentEventSourceAPI, json.RawMessage(`{"content":"do it"}`))
	require.NoError(t, err)
	require.NotNil(t, pending)

	approvalID := pending.ApprovalID

	decision, err := json.Marshal(ApprovalDecision{
		ApprovalID: approvalID,
		Approved:   false,
		Reason:     "not allowed",
	})
	require.NoError(t, err)
	pending, err = a.HandleEvent(ctx, enums.AgentEventSignalApproval, enums.AgentEventSourceAPI, decision)
	require.NoError(t, err)
	require.Nil(t, pending)

	published := pub.Published()
	data, ok := findApprovalResolvedPublication(published)
	require.True(t, ok, "expected approval_resolved PubSub message")
	require.Equal(t, "approval_resolved", data.Type)
	require.False(t, data.Approved)
	require.Equal(t, "not allowed", data.Reason)

	msgs := a.Messages()
	var rejected bool
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Name == "gated_tool" && tc.Result == "Rejected: not allowed" {
				rejected = true
			}
		}
	}
	require.True(t, rejected, "expected rejection result")

	last := msgs[len(msgs)-1]
	require.Equal(t, enums.RoleAssistant, last.Role)
	require.Equal(t, "understood, not doing it", last.Content)
}

func TestAgent_HandleEvent_approval_id_mismatch(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()
	a := newTestAgent(nil, el, pub)

	a.pendingApproval = &PendingApproval{
		ApprovalID: "correct-id",
		Calls:      []llm.ToolCall{{ID: "call-1", Name: "gated_tool", Arguments: "{}"}},
	}

	decision, err := json.Marshal(ApprovalDecision{
		ApprovalID: "wrong-id",
		Approved:   true,
	})
	require.NoError(t, err)
	_, err = a.HandleEvent(context.Background(), enums.AgentEventSignalApproval, enums.AgentEventSourceAPI, decision)
	require.Error(t, err)
	require.ErrorContains(t, err, "approval ID mismatch")
}

func TestAgent_HandleEvent_approval_no_pending(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()
	a := newTestAgent(nil, el, pub)

	decision, err := json.Marshal(ApprovalDecision{
		ApprovalID: "some-id",
		Approved:   true,
	})
	require.NoError(t, err)
	_, err = a.HandleEvent(context.Background(), enums.AgentEventSignalApproval, enums.AgentEventSourceAPI, decision)
	require.Error(t, err)
	require.ErrorContains(t, err, "no pending approval")
}

func TestAgent_HandleEvent_approval_fence_violation_preserves_suspension(t *testing.T) {
	t.Parallel()

	eventLog := newMemoryEventLog()
	eventLog.SetFenceToken("stream-1", 2)
	publisher := newMockPublisher()
	a := newTestAgent(nil, eventLog, publisher)
	a.pendingApproval = &PendingApproval{
		ApprovalID: "approval-1",
		Calls:      []llm.ToolCall{{ID: "call-1", Name: "gated_tool"}},
	}

	decision, err := json.Marshal(ApprovalDecision{
		ApprovalID: "approval-1",
		Approved:   true,
	})
	require.NoError(t, err)
	pending, err := a.HandleEvent(
		context.Background(),
		enums.AgentEventSignalApproval,
		enums.AgentEventSourceAPI,
		decision,
	)
	require.ErrorIs(t, err, eventlog.ErrFenceViolation)
	require.NotNil(t, pending)
	require.Equal(t, "approval-1", pending.ApprovalID)
	require.Len(t, publisher.Published(), 0)
}

func TestAgent_ReplayFromLog_pending_approval(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()

	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "do it"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role: enums.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "gated_tool", Arguments: "{}"},
		},
	})
	el.Seed("stream-1", enums.AgentEventApprovalRequested, PendingApproval{
		ApprovalID: "approval-123",
		Calls: []llm.ToolCall{
			{ID: "call-1", Name: "gated_tool", Arguments: "{}"},
		},
	})

	a := newTestAgent(nil, el, pub)

	pending, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)

	require.NotNil(t, pending)
	require.Equal(t, "approval-123", pending.ApprovalID)
	require.Len(t, pending.Calls, 1)
	require.Equal(t, "gated_tool", pending.Calls[0].Name)
}

func TestAgent_ReplayFromLog_resolved_approval(t *testing.T) {
	t.Parallel()

	el := newMemoryEventLog()
	pub := newMockPublisher()

	el.Seed("stream-1", enums.AgentEventSignalReceived, nil)
	el.Seed("stream-1", enums.AgentEventMessage, MessagePayload{Content: "do it"})
	el.Seed("stream-1", enums.AgentEventTurnStarted, nil)
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role: enums.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "gated_tool", Arguments: "{}"},
		},
	})
	el.Seed("stream-1", enums.AgentEventApprovalRequested, PendingApproval{
		ApprovalID: "approval-123",
		Calls: []llm.ToolCall{
			{ID: "call-1", Name: "gated_tool", Arguments: "{}"},
		},
	})
	el.Seed("stream-1", enums.AgentEventApprovalResolved, ApprovalResolvedPayload{
		ApprovalID: "approval-123",
		Approved:   true,
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "gated_tool", Arguments: "{}"},
		},
	})
	el.Seed("stream-1", enums.AgentEventToolResult, llm.Message{
		Role: enums.RoleTool,
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "gated_tool", Result: "gated tool executed"},
		},
	})
	el.Seed("stream-1", enums.AgentEventLLMResponse, llm.Message{
		Role:    enums.RoleAssistant,
		Content: "Done!",
	})
	el.Seed("stream-1", enums.AgentEventTurnCompleted, nil)

	a := newTestAgent(nil, el, pub)

	pending, err := a.ReplayFromLog(context.Background())
	require.NoError(t, err)

	require.Nil(t, pending)
}
