package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/llm"
	"nautilus/internal/ai/llm/anthropic"
	"nautilus/internal/database"
	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/optional"
	"nautilus/internal/pubsub"
)

// MessagePayload is the payload for signal.received events.
type MessagePayload struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content"`
}

// Agent manages conversation history and tool execution as a passive event processor.
// Events are delivered externally (by the Harness or tests); the Agent does not own
// any transport or run loop.
type Agent struct {
	streamID         string
	streamInternalID int
	fenceToken       int64

	db           database.Database
	clients      *llm.ClientRegistry
	defaultModel enums.Model
	logger       *log.Logger
	messages     []llm.Message
	toolBox      *llm.Toolbox

	eventLog  eventlog.EventLog
	publisher pubsub.Publisher

	// Handlers
	RepairHandler     RepairFunc
	MaxRepairAttempts int

	pendingApproval *PendingApproval

	// Current turn cancellation (guarded by mu)
	mu         sync.Mutex
	cancelTurn context.CancelFunc
}

// NewAgent creates a new Agent for the given stream.
func NewAgent(
	streamID string,
	streamInternalID int,
	fenceToken int64,
	db database.Database,
	clients *llm.ClientRegistry,
	tools []llm.Tool,
	eventLog eventlog.EventLog,
	publisher pubsub.Publisher,
) *Agent {
	logger := log.Default()
	return &Agent{
		streamID:          streamID,
		streamInternalID:  streamInternalID,
		fenceToken:        fenceToken,
		db:                db,
		logger:            logger,
		defaultModel:      anthropic.ClaudeSonnet45,
		clients:           clients,
		messages:          make([]llm.Message, 0),
		toolBox:           llm.NewToolbox(tools),
		eventLog:          eventLog,
		publisher:         publisher,
		MaxRepairAttempts: 1,
	}
}

// HandleEvent processes a single inbound event. The caller (typically the Harness)
// is responsible for transport and routing; the Agent just handles the event.
// This method blocks for the duration of the event (e.g. an entire LLM turn).
func (a *Agent) HandleEvent(
	ctx context.Context,
	eventType enums.AgentEventType,
	source enums.AgentEventSource,
	payload json.RawMessage,
) (pending *PendingApproval, err error) {
	defer func() {
		pending = a.pending()
		if r := recover(); r != nil {
			panicErr := InternalError(errors.Errorf("panic: %v", r))
			a.logger.Error("agent panic", "error", panicErr)
			if e := a.appendEvent(ctx, enums.AgentEventError, enums.AgentEventSourceAgent, panicErr); e != nil {
				a.logger.Error("failed to append panic error event", "error", e)
			}
			err = panicErr
		}
	}()

	switch eventType {
	case enums.AgentEventSignalReceived:
		var msg MessagePayload
		if err := json.Unmarshal(payload, &msg); err != nil {
			return pending, errors.Wrap(err, "unable to unmarshal message")
		}
		err = a.handleNewMessage(ctx, source, msg)

	case enums.AgentEventSignalApproval:
		var decision ApprovalDecision
		if err := json.Unmarshal(payload, &decision); err != nil {
			return pending, errors.Wrap(err, "unable to unmarshal approval decision")
		}
		err = a.handleApproval(ctx, source, decision)

	case enums.AgentEventSignalStop:
		a.Stop()

	default:
		err = errors.Errorf("unknown event type: %s", eventType)
	}
	return pending, err
}

// WithDefaultModel sets the default model for the agent.
func (a *Agent) WithDefaultModel(model enums.Model) *Agent {
	a.defaultModel = model
	return a
}

// Stop cancels the current turn. Safe to call from any goroutine.
func (a *Agent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancelTurn != nil {
		a.cancelTurn()
		a.cancelTurn = nil
	}
}

// handleNewMessage processes a new message signal.
func (a *Agent) handleNewMessage(ctx context.Context, source enums.AgentEventSource, msg MessagePayload) error {
	if msg.ID == "" {
		if e := a.appendEvent(ctx, enums.AgentEventSignalReceived, source, nil); e != nil {
			return e
		}

		if e := a.appendEvent(ctx, enums.AgentEventMessage, source, msg); e != nil {
			return e
		}

		a.messages = append(a.messages, llm.Message{
			Role:    enums.RoleUser,
			Content: msg.Content,
		})

		return a.executeTurn(ctx, "")
	}

	// Identified messages were durably accepted before queue delivery.
	if _, e := a.ReplayFromLog(ctx); e != nil {
		return e
	}

	return a.executeTurn(ctx, msg.ID)
}

func (a *Agent) pending() *PendingApproval {
	if a.pendingApproval == nil {
		return nil
	}
	pending := *a.pendingApproval
	pending.Calls = append([]llm.ToolCall(nil), pending.Calls...)
	return &pending
}

// handleApproval processes an approval resolution signal.
func (a *Agent) handleApproval(ctx context.Context, source enums.AgentEventSource, decision ApprovalDecision) error {
	if a.pendingApproval == nil {
		return errors.New("no pending approval to resolve")
	}

	if decision.ApprovalID != a.pendingApproval.ApprovalID {
		return errors.Errorf("approval ID mismatch: expected %s, got %s", a.pendingApproval.ApprovalID, decision.ApprovalID)
	}

	resolvedPayload := ApprovalResolvedPayload{
		ApprovalID: decision.ApprovalID,
		Approved:   decision.Approved,
		Reason:     decision.Reason,
		ToolCalls:  a.pendingApproval.Calls,
		Approver:   decision.Approver,
	}
	if e := a.appendEvent(ctx, enums.AgentEventApprovalResolved, source, resolvedPayload); e != nil {
		return e
	}
	a.publishApprovalResolved(ctx, resolvedPayload)

	calls := a.pendingApproval.Calls
	a.pendingApproval = nil

	reason := optional.Empty[string]()
	if decision.Reason != "" {
		reason = optional.Set(decision.Reason)
	}

	if decision.Approved {
		return a.executeApprovedCalls(ctx, calls, reason)
	}
	return a.rejectCalls(ctx, calls, reason)
}

// executeApprovedCalls runs previously gated tool calls and continues the turn.
// If a comment is provided, it is injected as a user message after the tool results.
func (a *Agent) executeApprovedCalls(ctx context.Context, calls []llm.ToolCall, comment optional.Optional[string]) error {
	executedCalls := make([]llm.ToolCall, 0, len(calls))
	var newTools []llm.Tool

	for i := range calls {
		result, tools := a.toolBox.ExecuteCall(ctx, &calls[i], calls[i].Arguments)
		executedCalls = append(executedCalls, *result)
		newTools = append(newTools, tools...)
	}

	a.toolBox.Add(newTools)
	if e := a.appendEvent(ctx, enums.AgentEventToolCall, enums.AgentEventSourceAgent, executedCalls); e != nil {
		a.logger.Error("failed to append approved tool calls event", "error", e)
	}

	toolMessages := make([]llm.Message, 0, len(executedCalls))
	for _, call := range executedCalls {
		msg := call.Message()
		toolMessages = append(toolMessages, msg)
		if e := a.appendEvent(ctx, enums.AgentEventToolResult, enums.AgentEventSourceAgent, msg); e != nil {
			a.logger.Error("failed to append approved tool result event", "error", e)
		}
	}
	a.messages = append(a.messages, toolMessages...)

	if comment.Set {
		content := fmt.Sprintf("[Approval comment from user]: %s", comment.Data)
		a.messages = append(a.messages, llm.Message{
			Role:    enums.RoleUser,
			Content: content,
		})
		if e := a.appendEvent(ctx, enums.AgentEventMessage, enums.AgentEventSourceInternal, MessagePayload{
			Content: content,
		}); e != nil {
			a.logger.Error("failed to append approval comment event", "error", e)
		}
	}

	return a.executeTurn(ctx, "")
}

// rejectCalls creates rejection results for gated tool calls and continues the turn.
func (a *Agent) rejectCalls(ctx context.Context, calls []llm.ToolCall, reason optional.Optional[string]) error {
	rejectionReason := reason.Or("The user rejected this tool call.")

	rejectedCalls := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		rejectedCalls = append(rejectedCalls, llm.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: call.Arguments,
			Result:    fmt.Sprintf("Rejected: %s", rejectionReason),
		})
	}

	if e := a.appendEvent(ctx, enums.AgentEventToolCall, enums.AgentEventSourceAgent, rejectedCalls); e != nil {
		a.logger.Error("failed to append rejected tool calls event", "error", e)
	}

	toolMessages := make([]llm.Message, 0, len(rejectedCalls))
	for _, call := range rejectedCalls {
		msg := call.Message()
		toolMessages = append(toolMessages, msg)
		if e := a.appendEvent(ctx, enums.AgentEventToolResult, enums.AgentEventSourceAgent, msg); e != nil {
			a.logger.Error("failed to append rejected tool result event", "error", e)
		}
	}
	a.messages = append(a.messages, toolMessages...)

	return a.executeTurn(ctx, "")
}

// executeTurn runs a single conversational turn with the LLM.
func (a *Agent) executeTurn(ctx context.Context, idempotencyKey string) error {
	turnCtx, cancel := context.WithCancel(ctx)

	a.mu.Lock()
	a.cancelTurn = cancel
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cancelTurn = nil
		a.mu.Unlock()
	}()

	if idempotencyKey == "" {
		if e := a.appendEvent(turnCtx, enums.AgentEventTurnStarted, enums.AgentEventSourceAgent, nil); e != nil {
			return e
		}
	} else {
		if e := a.appendEventWithKey(turnCtx, enums.AgentEventTurnStarted, enums.AgentEventSourceAgent, nil, idempotencyKey); e != nil {
			return e
		}
	}

	client, err := a.clients.Get(a.defaultModel)
	if err != nil {
		return err
	}

	// Loop for tool calling
	for range 100 {
		stream, err := client.StreamCompletion(turnCtx, &llm.Request{
			Model:    a.defaultModel,
			Messages: a.messages,
			Tools:    a.toolBox.Tools(),
		})
		if err != nil {
			if e := a.appendEvent(turnCtx, enums.AgentEventError, enums.AgentEventSourceAgent, LLMRequestError(err)); e != nil {
				a.logger.Error("failed to append event", "error", e)
			}
			return err
		}

		response := llm.Message{
			Role:        enums.RoleAssistant,
			ToolCalls:   make([]llm.ToolCall, 0),
			Attachments: make([]llm.Attachment, 0),
		}

		// Process tokens — accumulate into response and forward via PubSub.
		// Individual tokens are NOT written to the EventLog; only the complete
		// llm.response is persisted once the stream finishes.
		for token := range stream.Tokens() {
			tokenType := token.TokenType()

			if tokenType == enums.TokenTypeError {
				errToken := token.(*llm.ErrorToken)
				if e := a.appendEvent(turnCtx, enums.AgentEventError, enums.AgentEventSourceAgent, LLMRequestError(errToken.Err)); e != nil {
					a.logger.Error("failed to append token error event", "error", e)
				}
				return errToken.Err
			}

			a.publishToken(turnCtx, token)

			switch tokenType {
			case enums.TokenTypeText:
				response.Content += token.Content()

			case enums.TokenTypeReasoning:
				response.Reasoning += token.Content()

			case enums.TokenTypeToolCall:
				tc := token.(*llm.ToolCallToken)
				if err := a.toolBox.HandleToken(tc); err != nil {
					if e := a.appendEvent(turnCtx, enums.AgentEventError, enums.AgentEventSourceAgent, ToolExecutionError(err)); e != nil {
						a.logger.Error("failed to append tool handling error event", "error", e)
					}
					return err
				}

			case enums.TokenTypeUsage:
				ut := token.(*llm.UsageToken)
				response.Usage = &llm.Usage{
					InputTokens:  ut.InputTokens,
					OutputTokens: ut.OutputTokens,
					TotalTokens:  ut.TotalTokens,
				}
			}
		}

		// If any buffered call requires approval, suspend the turn
		if a.toolBox.HasApprovalRequired() {
			pendingCalls := a.toolBox.PendingCalls()
			a.toolBox.ClearBuffers()

			response.ToolCalls = pendingCalls
			a.messages = append(a.messages, response)
			if e := a.appendEvent(turnCtx, enums.AgentEventLLMResponse, enums.AgentEventSourceAgent, response); e != nil {
				return e
			}

			approval, err := agentapprovals.Create(turnCtx, a.db, a.streamInternalID, a.fenceToken, pendingCalls)
			if err != nil {
				return errors.Wrap(err, "unable to create approval")
			}

			pending := &PendingApproval{
				ApprovalID: approval.ExternalID,
				Calls:      pendingCalls,
			}
			if e := a.appendEvent(turnCtx, enums.AgentEventApprovalRequested, enums.AgentEventSourceAgent, pending); e != nil {
				return e
			}
			a.publishApprovalRequested(turnCtx, pending)

			a.pendingApproval = pending
			return nil
		}

		// Flush toolbox to execute tool calls
		toolResults, err := a.toolBox.Flush(turnCtx)
		if err != nil {
			if e := a.appendEvent(turnCtx, enums.AgentEventError, enums.AgentEventSourceAgent, ToolExecutionError(err)); e != nil {
				a.logger.Error("failed to append flush error event", "error", e)
			}
			return err
		}
		response.ToolCalls = append(response.ToolCalls, toolResults.Calls...)

		// Attempt to repair any failed tool calls
		if a.RepairHandler != nil {
			repairTools := a.repairToolCalls(turnCtx, response.ToolCalls)
			a.toolBox.Add(repairTools)
		}

		// Filter out skipped calls
		filteredCalls := make([]llm.ToolCall, 0, len(response.ToolCalls))
		for _, call := range response.ToolCalls {
			if call.Result == "" && call.Error == "" {
				continue
			}
			filteredCalls = append(filteredCalls, call)
		}
		response.ToolCalls = filteredCalls

		a.toolBox.AddToolResult(toolResults)
		a.messages = append(a.messages, response)

		for _, call := range response.ToolCalls {
			a.publishToolResult(turnCtx, call)
		}

		// Log the complete response to the EventLog (once per round)
		if e := a.appendEvent(turnCtx, enums.AgentEventLLMResponse, enums.AgentEventSourceAgent, response); e != nil {
			a.logger.Error("failed to append llm response event", "error", e)
		}

		// If no tool calls, turn is complete
		if len(response.ToolCalls) == 0 {
			if e := a.appendEvent(turnCtx, enums.AgentEventTurnCompleted, enums.AgentEventSourceAgent, nil); e != nil {
				a.logger.Error("failed to append turn completed event", "error", e)
			}
			return nil
		}

		// Log tool calls and results
		if e := a.appendEvent(turnCtx, enums.AgentEventToolCall, enums.AgentEventSourceAgent, toolResults.Calls); e != nil {
			a.logger.Error("failed to append tool call event", "error", e)
		}

		toolMessages := make([]llm.Message, 0, len(response.ToolCalls))
		for _, call := range response.ToolCalls {
			msg := call.Message()
			toolMessages = append(toolMessages, msg)
			if e := a.appendEvent(turnCtx, enums.AgentEventToolResult, enums.AgentEventSourceAgent, msg); e != nil {
				a.logger.Error("failed to append tool result event", "error", e)
			}
		}
		a.messages = append(a.messages, toolMessages...)

		// Continue loop for next turn
	}

	return errors.New("tool call loop limit exceeded")
}

// repairToolCalls attempts to repair any failed tool calls using the RepairHandler.
func (a *Agent) repairToolCalls(ctx context.Context, calls []llm.ToolCall) []llm.Tool {
	if a.RepairHandler == nil {
		return nil
	}

	maxAttempts := a.MaxRepairAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var newTools []llm.Tool

	for i := range calls {
		call := &calls[i]
		if call.ErrorKind == llm.ToolCallErrorNone {
			continue
		}

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			var tool *llm.Tool
			if call.ErrorKind != llm.ToolCallErrorNotFound {
				tool = a.toolBox.GetTool(call.Name)
			}

			var toolErr *ToolError
			switch call.ErrorKind {
			case llm.ToolCallErrorNotFound:
				toolErr = NewToolNotFoundError(call.Name)
			case llm.ToolCallErrorInvalidArguments:
				toolErr = NewInvalidArgumentsError(call.Name, errors.New(call.Error))
			case llm.ToolCallErrorExecution:
				toolErr = NewExecutionError(call.Name, errors.New(call.Error))
			}

			info := &RepairInfo{
				ToolCall: call,
				Tool:     tool,
				Error:    toolErr,
				Messages: a.messages,
				Attempt:  attempt,
			}

			repaired, err := a.RepairHandler(ctx, info)
			if err != nil {
				break
			}

			if repaired == nil {
				break
			}

			if repaired.Skip {
				call.Error = ""
				call.ErrorKind = llm.ToolCallErrorNone
				call.Result = ""
				break
			}

			repairedCall, tools := a.toolBox.ExecuteCall(ctx, call, repaired.Arguments)
			call.Arguments = repairedCall.Arguments
			call.Result = repairedCall.Result
			call.Error = repairedCall.Error
			call.ErrorKind = repairedCall.ErrorKind

			if repairedCall.ErrorKind == llm.ToolCallErrorNone {
				newTools = append(newTools, tools...)
				break
			}
		}
	}

	return newTools
}

// appendEvent appends an event to the event log with fence token validation.
// If the payload is an error, it is serialized via serializeError so callers
// can pass raw errors without manual map construction.
// Error events are automatically published to PubSub for live streaming.
func (a *Agent) appendEvent(ctx context.Context, eventType enums.AgentEventType, source enums.AgentEventSource, payload any) error {
	return a.appendEventWithKey(ctx, eventType, source, payload, "")
}

func (a *Agent) appendEventWithKey(ctx context.Context, eventType enums.AgentEventType, source enums.AgentEventSource, payload any, idempotencyKey string) error {
	if err, ok := payload.(error); ok {
		payload = serializeError(err)
	}

	_, err := a.eventLog.Append(ctx, a.streamID, eventType, source, payload, eventlog.Tokens{
		Fence:       a.fenceToken,
		Idempotency: idempotencyKey,
	})
	if err != nil {
		if errors.Is(err, eventlog.ErrFenceViolation) {
			return err
		}
		a.logger.Error("failed to append event", "error", err)
		return err
	}

	// Publish error events to PubSub for real-time delivery
	if eventType == enums.AgentEventError {
		if errPayload, ok := payload.(errorPayload); ok {
			a.publishError(ctx, errPayload.Error)
		}
	}

	return nil
}

// publishToken publishes a token to the pubsub channel for live streaming.
func (a *Agent) publishToken(ctx context.Context, token llm.Token) {
	topic := fmt.Sprintf("agent:%s", a.streamID)

	data := TokenEventPayload{
		Type:    token.TokenType(),
		Content: token.Content(),
	}

	if tc, ok := token.(*llm.ToolCallToken); ok {
		data.ID = tc.ID
		data.Name = tc.Name
		data.Index = &tc.Index
	}

	if err := a.publisher.Publish(ctx, topic, data); err != nil {
		a.logger.Error("failed to publish token", "error", err)
	}
}

// publishApprovalRequested publishes an approval.requested event to PubSub.
func (a *Agent) publishApprovalRequested(ctx context.Context, pending *PendingApproval) {
	topic := fmt.Sprintf("agent:%s", a.streamID)

	data := ApprovalRequestedEventPayload{
		Type:       "approval_requested",
		ApprovalID: pending.ApprovalID,
		ToolCalls:  pending.Calls,
	}

	if err := a.publisher.Publish(ctx, topic, data); err != nil {
		a.logger.Error("failed to publish approval requested", "error", err)
	}
}

// publishApprovalResolved publishes an approval.resolved event to PubSub.
func (a *Agent) publishApprovalResolved(ctx context.Context, resolved ApprovalResolvedPayload) {
	topic := fmt.Sprintf("agent:%s", a.streamID)

	data := ApprovalResolvedEventPayload{
		Type:       "approval_resolved",
		ApprovalID: resolved.ApprovalID,
		Approved:   resolved.Approved,
		Reason:     resolved.Reason,
		ToolCalls:  resolved.ToolCalls,
		Approver:   resolved.Approver,
	}

	if err := a.publisher.Publish(ctx, topic, data); err != nil {
		a.logger.Error("failed to publish approval resolved", "error", err)
	}
}

// publishToolResult publishes a completed tool call result to PubSub so
// live-streaming clients see it without waiting for event log replay.
func (a *Agent) publishToolResult(ctx context.Context, call llm.ToolCall) {
	topic := fmt.Sprintf("agent:%s", a.streamID)

	data := ToolResultEventPayload{
		Type:   "tool_result",
		ID:     call.ID,
		Name:   call.Name,
		Result: call.Result,
		Error:  call.Error,
	}

	if err := a.publisher.Publish(ctx, topic, data); err != nil {
		a.logger.Error("failed to publish tool result", "error", err)
	}
}

// publishError publishes an error event to PubSub so live-streaming clients
// see errors in real-time without waiting for event log replay.
func (a *Agent) publishError(ctx context.Context, errMsg string) {
	topic := fmt.Sprintf("agent:%s", a.streamID)

	data := ErrorEventPayload{
		Type:    "error",
		Content: errMsg,
	}

	if err := a.publisher.Publish(ctx, topic, data); err != nil {
		a.logger.Error("failed to publish error", "error", err)
	}
}

// Messages returns a copy of the agent's current message history.
func (a *Agent) Messages() []llm.Message {
	msgs := make([]llm.Message, len(a.messages))
	copy(msgs, a.messages)
	return msgs
}

// ReplayFromLog reconstructs the agent's message history from the event log.
// It iterates events in sequence order and rebuilds the messages slice from:
//   - message          -> user message
//   - llm.response     -> assistant message (full content + tool calls)
//   - tool.result      -> tool result message
//
// Signal events (signal.received) are markers and carry no content.
// Streaming-granularity events (llm.text, llm.reasoning) are skipped since
// the full content is captured in the llm.response event.
func (a *Agent) ReplayFromLog(ctx context.Context) (*PendingApproval, error) {
	events, err := a.eventLog.List(ctx, a.streamID, 0)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list events for replay")
	}

	a.messages = make([]llm.Message, 0)
	a.pendingApproval = nil

	for _, event := range events {
		switch event.Type {
		case enums.AgentEventMessage:
			var msg MessagePayload
			if err := json.Unmarshal(event.Payload, &msg); err != nil {
				return nil, errors.Wrap(err, "unable to unmarshal message payload")
			}
			a.messages = append(a.messages, llm.Message{
				Role:    enums.RoleUser,
				Content: msg.Content,
			})

		case enums.AgentEventLLMResponse:
			var msg llm.Message
			if err := json.Unmarshal(event.Payload, &msg); err != nil {
				return nil, errors.Wrap(err, "unable to unmarshal llm.response payload")
			}
			a.messages = append(a.messages, msg)

		case enums.AgentEventToolResult:
			var msg llm.Message
			if err := json.Unmarshal(event.Payload, &msg); err != nil {
				return nil, errors.Wrap(err, "unable to unmarshal tool.result payload")
			}
			a.messages = append(a.messages, msg)

		case enums.AgentEventApprovalRequested:
			var pending PendingApproval
			if err := json.Unmarshal(event.Payload, &pending); err != nil {
				return nil, errors.Wrap(err, "unable to unmarshal approval.requested payload")
			}
			a.pendingApproval = &pending

		case enums.AgentEventApprovalResolved:
			a.pendingApproval = nil
		}
	}

	a.logger.Info("replayed agent from event log", "stream_id", a.streamID, "events", len(events), "messages", len(a.messages))
	return a.pending(), nil
}
