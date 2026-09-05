package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/llm"
	"nautilus/internal/database"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/notifier"
	"nautilus/internal/pubsub"
)

const defaultIdleTimeout = 2 * time.Minute

// QueueEvent is the wire format for events published to and consumed from
// the agent signals queue. Both the HTTP handlers (publisher) and the
// Harness (consumer) use this type.
type QueueEvent struct {
	StreamID string                 `json:"stream_id"`
	Type     enums.AgentEventType   `json:"type"`
	Source   enums.AgentEventSource `json:"source,omitempty"`
	Payload  json.RawMessage        `json:"payload,omitempty"`
}

// Harness manages agent lifecycle and routes queue events to agent instances.
type Harness struct {
	db          database.Database
	eventLog    eventlog.EventLog
	publisher   pubsub.Publisher
	clients     *llm.ClientRegistry
	tools       []llm.Tool
	logger      *log.Logger
	notifier    notifier.Notifier
	idleTimeout time.Duration
	ctx         context.Context
	cancel      context.CancelFunc

	mu           sync.RWMutex
	wg           sync.WaitGroup
	shuttingDown bool
	agents       map[string]*agentStream // streamID (external) -> running agent
}

// agentStream represents a starting or running agent instance. The internal events channel
// is a Harness implementation detail for buffering work to a per-agent goroutine.
type agentStream struct {
	userExternalID string
	agent          *Agent
	lifecycle      *lifecycle
	events         chan agentEvent
	cancel         context.CancelFunc
	ready          chan struct{}
	startErr       error
	mu             sync.Mutex
}

// agentEvent is the internal representation of an event forwarded to an agent goroutine.
type agentEvent struct {
	eventType enums.AgentEventType
	source    enums.AgentEventSource
	payload   json.RawMessage
}

func (as *agentStream) forward(ctx context.Context, event agentEvent) error {
	select {
	case as.events <- event:
	case <-as.ready:
		if as.startErr != nil {
			return as.startErr
		}
		select {
		case as.events <- event:
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "context canceled while forwarding event to running agent")
		}
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "context canceled while forwarding event to running agent")
	}

	select {
	case <-as.ready:
		return as.startErr
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "context canceled while starting agent")
	}
}

func (as *agentStream) started(err error) {
	as.startErr = err
	close(as.ready)
}

func (as *agentStream) setAgent(agent *Agent) {
	as.mu.Lock()
	as.agent = agent
	as.mu.Unlock()
}

func (as *agentStream) stop() {
	as.cancel()

	as.mu.Lock()
	agent := as.agent
	as.mu.Unlock()
	if agent != nil {
		agent.Stop()
	}
}

// NewHarness creates a new Harness.
func NewHarness(
	db database.Database,
	eventLog eventlog.EventLog,
	publisher pubsub.Publisher,
	clients *llm.ClientRegistry,
	tools []llm.Tool,
	logger *log.Logger,
	notifier notifier.Notifier,
) *Harness {
	ctx, cancel := context.WithCancel(context.Background())
	return &Harness{
		db:          db,
		eventLog:    eventLog,
		publisher:   publisher,
		clients:     clients,
		tools:       tools,
		logger:      logger,
		notifier:    notifier,
		idleTimeout: defaultIdleTimeout,
		ctx:         ctx,
		cancel:      cancel,
		agents:      make(map[string]*agentStream),
	}
}

// WithIdleTimeout returns the Harness with a custom idle timeout.
func (h *Harness) WithIdleTimeout(d time.Duration) *Harness {
	h.idleTimeout = d
	return h
}

// HandleQueueMessage implements queue.MessageHandler. It deserializes a
// QueueEvent from the queue and routes it to the appropriate agent.
func (h *Harness) HandleQueueMessage(ctx context.Context, data []byte) error {
	var qe QueueEvent
	if err := json.Unmarshal(data, &qe); err != nil {
		return errors.Wrap(err, "unable to unmarshal queue event")
	}

	if qe.StreamID == "" {
		return errors.New("queue event missing stream_id")
	}

	// Stop events bypass the event buffer and cancel directly
	if qe.Type == enums.AgentEventSignalStop {
		return h.stopAgent(ctx, qe.StreamID)
	}

	ev := agentEvent{eventType: qe.Type, source: qe.Source, payload: qe.Payload}

	h.mu.RLock()
	running, exists := h.agents[qe.StreamID]
	shuttingDown := h.shuttingDown
	h.mu.RUnlock()

	if shuttingDown {
		return errors.New("harness is shutting down")
	}
	if exists {
		return running.forward(ctx, ev)
	}

	return h.startAgent(ctx, qe.StreamID, ev)
}

// startAgent cold-starts a new agent for the given stream.
func (h *Harness) startAgent(ctx context.Context, streamID string, initial agentEvent) error {
	agentCtx, cancel := context.WithCancel(h.ctx)
	initCtx, cancelInit := context.WithCancel(agentCtx)
	stopCaller := context.AfterFunc(ctx, cancelInit)
	defer func() {
		stopCaller()
		cancelInit()
	}()

	running := &agentStream{
		events: make(chan agentEvent, 10),
		cancel: cancel,
		ready:  make(chan struct{}),
	}
	running.events <- initial

	h.mu.Lock()
	if h.shuttingDown {
		h.mu.Unlock()
		cancel()
		return errors.New("harness is shutting down")
	}

	// Double-check under write lock
	if existing, exists := h.agents[streamID]; exists {
		h.mu.Unlock()
		cancel()
		return existing.forward(ctx, initial)
	}
	h.agents[streamID] = running
	h.wg.Add(1)
	h.mu.Unlock()

	stream, err := agentstreams.GetByExternalID(initCtx, h.db, streamID)
	if err != nil {
		err = errors.Wrap(err, "unable to get stream")
		h.failStart(streamID, running, err)
		return err
	}
	if stream == nil {
		err = errors.Errorf("stream not found: %s", streamID)
		h.failStart(streamID, running, err)
		return err
	}

	fenceToken, err := agentstreams.AcquireFence(initCtx, h.db, stream.ID)
	if err != nil {
		err = errors.Wrap(err, "unable to acquire fence token")
		h.failStart(streamID, running, err)
		return err
	}
	running.lifecycle = newLifecycle(h.db, stream.ID, fenceToken, agentstreams.StatusRunning)

	user, err := users.Get(initCtx, h.db, stream.UserID)
	if err != nil {
		err = errors.Wrap(err, "unable to get stream owner")
		h.failStart(streamID, running, err)
		return err
	}
	if user == nil {
		err = errors.Errorf("stream owner not found: user_id=%d", stream.UserID)
		h.failStart(streamID, running, err)
		return err
	}

	h.logger.Info("starting agent", "stream_id", streamID, "fence_token", fenceToken)

	ag := NewAgent(streamID, stream.ID, fenceToken, h.db, h.clients, h.tools, h.eventLog, h.publisher)

	pending, err := ag.ReplayFromLog(initCtx)
	if err != nil {
		err = errors.Wrap(err, "unable to replay agent state")
		h.failStart(streamID, running, err)
		return err
	}
	if _, _, err := running.lifecycle.reconcile(initCtx, pending); err != nil {
		h.failStart(streamID, running, err)
		return err
	}
	running.userExternalID = user.ExternalID
	running.setAgent(ag)
	go func() {
		defer h.wg.Done()
		h.runAgentLoop(agentCtx, streamID, running)
	}()

	running.started(nil)
	return nil
}

func (h *Harness) failStart(streamID string, running *agentStream, startErr error) {
	running.stop()
	if running.lifecycle != nil && !errors.Is(startErr, agentstreams.ErrFenceViolation) {
		event := lifecycleFail
		if errors.Is(startErr, context.Canceled) || errors.Is(startErr, context.DeadlineExceeded) {
			event = lifecycleCancel
		}
		if err := running.lifecycle.transition(context.Background(), event); err != nil &&
			!errors.Is(err, agentstreams.ErrFenceViolation) {
			h.logger.Error("failed to project cold-start failure", "stream_id", streamID, "error", err)
		}
	}
	running.started(startErr)
	h.removeAgent(streamID, running)
	h.wg.Done()
}

func (h *Harness) removeAgent(streamID string, running *agentStream) {
	h.mu.Lock()
	if h.agents[streamID] == running {
		delete(h.agents, streamID)
	}
	h.mu.Unlock()
}

// runAgentLoop drains the internal event buffer and calls Agent.HandleEvent
// for each event sequentially. When no events arrive within the idle timeout,
// the agent is evicted from memory and the stream status is set to idle.
// Idle timeout is suppressed while the agent has a pending approval.
func (h *Harness) runAgentLoop(ctx context.Context, streamID string, as *agentStream) {
	exitEvent := lifecycleIdle
	ag := as.agent
	lifecycle := as.lifecycle

	defer func() {
		h.removeAgent(streamID, as)

		if exitEvent != "" {
			if err := lifecycle.transition(context.Background(), exitEvent); err != nil {
				if errors.Is(err, agentstreams.ErrFenceViolation) {
					h.logger.Info("agent lost fence ownership", "stream_id", streamID)
				} else {
					h.logger.Error("failed to project agent lifecycle", "stream_id", streamID, "error", err)
				}
			}
		}

		h.logger.Info("agent stopped", "stream_id", streamID, "status", lifecycle.status)
	}()

	for {
		// Suppress idle timeout while awaiting approval
		var timeout <-chan time.Time
		if lifecycle.canIdle() {
			timeout = time.After(h.idleTimeout)
		}

		select {
		case <-ctx.Done():
			exitEvent = lifecycleCancel
			return

		case ev, ok := <-as.events:
			if !ok {
				exitEvent = lifecycleCancel
				return
			}

			pending, err := ag.HandleEvent(ctx, ev.eventType, ev.source, ev.payload)
			if err != nil {
				if errors.Is(err, eventlog.ErrFenceViolation) ||
					errors.Is(err, agentstreams.ErrFenceViolation) {
					h.logger.Info("fence violation, shutting down agent", "stream_id", streamID)
					exitEvent = ""
					return
				}
				h.logger.Error("agent event error", "stream_id", streamID, "error", err)
			}
			if ctx.Err() != nil {
				exitEvent = lifecycleCancel
				return
			}

			pending, requested, err := lifecycle.reconcile(ctx, pending)
			if err != nil {
				if errors.Is(err, agentstreams.ErrFenceViolation) {
					h.logger.Info("fence violation, shutting down agent", "stream_id", streamID)
					exitEvent = ""
					return
				}
				h.logger.Error("agent lifecycle error", "stream_id", streamID, "error", err)
				exitEvent = lifecycleFail
				return
			}
			if requested {
				h.sendApprovalNotification(ctx, streamID, as.userExternalID, pending)
			}

		case <-timeout:
			exitEvent = lifecycleIdle
			return
		}
	}
}

// sendApprovalNotification sends a web push notification for a pending approval.
func (h *Harness) sendApprovalNotification(ctx context.Context, streamID, recipient string, pending *PendingApproval) {
	if h.notifier == nil {
		return
	}

	toolNames := make([]string, 0, len(pending.Calls))
	for _, call := range pending.Calls {
		toolNames = append(toolNames, call.Name)
	}

	err := h.notifier.Notify(ctx, &notifier.Notification{
		Recipient: recipient,
		Subject:   "Agent requires approval",
		Body:      fmt.Sprintf("Tools awaiting approval: %v", toolNames),
		Urgency:   notifier.UrgencyHigh,
		Metadata: map[string]string{
			"stream_id":   streamID,
			"approval_id": pending.ApprovalID,
		},
	})
	if err != nil {
		h.logger.Error("failed to send approval notification", "stream_id", streamID, "error", err)
	}
}

// Shutdown stops all running agents and waits for their loops to exit.
func (h *Harness) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.shuttingDown = true
	h.cancel()

	running := make([]*agentStream, 0, len(h.agents))
	for _, as := range h.agents {
		running = append(running, as)
	}
	h.mu.Unlock()

	for _, as := range running {
		as.stop()
	}

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "context canceled while shutting down harness")
	}
}

// stopAgent stops a running agent or marks an existing idle stream as cancelled.
func (h *Harness) stopAgent(ctx context.Context, streamID string) error {
	h.mu.RLock()
	running, exists := h.agents[streamID]
	h.mu.RUnlock()

	if !exists {
		stream, err := agentstreams.GetByExternalID(ctx, h.db, streamID)
		if err != nil {
			return errors.Wrap(err, "unable to get stream")
		}
		if stream == nil {
			return errors.Errorf("stream not found: %s", streamID)
		}
		lifecycle := newLifecycle(h.db, stream.ID, stream.FenceToken, stream.Status)
		return lifecycle.transition(ctx, lifecycleCancel)
	}

	running.stop()

	return nil
}
