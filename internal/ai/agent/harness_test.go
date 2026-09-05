package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/llm"
	"nautilus/internal/ai/llm/anthropic"
	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func marshalQueueEvent(t *testing.T, qe QueueEvent) []byte {
	t.Helper()
	data, err := json.Marshal(qe)
	require.NoError(t, err)
	return data
}

func marshalPayload(t *testing.T, payload any) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return data
}

type failingListEventLog struct {
	*memoryEventLog
}

func (l *failingListEventLog) List(context.Context, string, int64) ([]*eventlog.Event, error) {
	return nil, errors.New("replay failed")
}

type blockingReplayEventLog struct {
	*memoryEventLog
	streamID string
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func newBlockingReplayEventLog(streamID string) *blockingReplayEventLog {
	return &blockingReplayEventLog{
		memoryEventLog: newMemoryEventLog(),
		streamID:       streamID,
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
}

func (l *blockingReplayEventLog) List(ctx context.Context, streamID string, afterSequence int64) ([]*eventlog.Event, error) {
	if streamID == l.streamID {
		l.once.Do(func() { close(l.started) })
		select {
		case <-l.release:
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "replay interrupted")
		}
	}
	return l.memoryEventLog.List(ctx, streamID, afterSequence)
}

type blockingClient struct {
	started  chan struct{}
	released chan struct{}
}

func newBlockingClient() *blockingClient {
	return &blockingClient{
		started:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (c *blockingClient) StreamCompletion(ctx context.Context, _ *llm.Request) (llm.TokenStream, error) {
	close(c.started)
	<-ctx.Done()
	close(c.released)
	return nil, errors.Wrap(ctx.Err(), "completion canceled")
}

func (c *blockingClient) Completion(context.Context, *llm.Request) (*llm.Message, error) {
	return nil, nil
}

func waitForAgentEviction(t *testing.T, h *Harness, streamID string) {
	t.Helper()

	require.Eventually(t, func() bool {
		h.mu.RLock()
		_, exists := h.agents[streamID]
		h.mu.RUnlock()
		return !exists
	}, time.Second, 10*time.Millisecond)

	h.wg.Wait()
}

func TestHarness_HandleQueueMessage_cold_start(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()
	client := newStreamingMockClient(simpleTextTokens("hello from agent"))
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, nil, log.Default(), nil)

	payload := marshalPayload(t, MessagePayload{Content: "hello"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})

	err := h.HandleQueueMessage(ctx, msg)
	require.NoError(t, err)

	// Give agent goroutine time to process
	time.Sleep(100 * time.Millisecond)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusRunning, updated.Status)
	require.Greater(t, updated.FenceToken, int64(0))
}

func TestHarness_coldStartsDifferentStreamsIndependently(t *testing.T) {
	db := testutil.SetupTestDBWithCommit(t)
	ctx := context.Background()
	first := testutil.CreateTestStream(t, db)
	second := testutil.CreateTestStream(t, db)

	eventLog := newBlockingReplayEventLog(first.ExternalID)
	t.Cleanup(func() {
		eventLog.once.Do(func() { close(eventLog.started) })
		select {
		case <-eventLog.release:
		default:
			close(eventLog.release)
		}
	})

	client := newStreamingMockClient(
		simpleTextTokens("first"),
		simpleTextTokens("second"),
	)
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)
	h := NewHarness(db, eventLog, newMockPublisher(), clients, nil, log.Default(), nil)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, h.Shutdown(shutdownCtx))
	})

	firstMessage := marshalQueueEvent(t, QueueEvent{
		StreamID: first.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  marshalPayload(t, MessagePayload{Content: "first"}),
	})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.HandleQueueMessage(ctx, firstMessage)
	}()

	select {
	case <-eventLog.started:
	case <-time.After(time.Second):
		t.Fatal("first replay did not start")
	}

	secondMessage := marshalQueueEvent(t, QueueEvent{
		StreamID: second.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  marshalPayload(t, MessagePayload{Content: "second"}),
	})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- h.HandleQueueMessage(ctx, secondMessage)
	}()

	select {
	case err := <-secondDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second stream waited for unrelated replay")
	}

	close(eventLog.release)
	require.NoError(t, <-firstDone)
}

func TestHarness_ShutdownCancelsColdStart(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	eventLog := newBlockingReplayEventLog(stream.ExternalID)
	h := NewHarness(db, eventLog, nil, nil, nil, log.Default(), nil)

	message := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  marshalPayload(t, MessagePayload{Content: "hello"}),
	})
	startDone := make(chan error, 1)
	go func() {
		startDone <- h.HandleQueueMessage(context.Background(), message)
	}()

	select {
	case <-eventLog.started:
	case <-time.After(time.Second):
		t.Fatal("replay did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, h.Shutdown(shutdownCtx))
	require.ErrorIs(t, <-startDone, context.Canceled)

	updated, err := agentstreams.Get(t.Context(), db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusCancelled, updated.Status)
}

func TestHarness_HandleQueueMessage_forward(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()
	client := newStreamingMockClient(
		simpleTextTokens("first response"),
		simpleTextTokens("second response"),
	)
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, nil, log.Default(), nil)

	payload1 := marshalPayload(t, MessagePayload{Content: "hello"})
	msg1 := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload1,
	})
	err := h.HandleQueueMessage(ctx, msg1)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	payload2 := marshalPayload(t, MessagePayload{Content: "follow-up"})
	msg2 := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload2,
	})
	err = h.HandleQueueMessage(ctx, msg2)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), updated.FenceToken)
}

func TestHarness_HandleQueueMessage_stop(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()
	client := newStreamingMockClient(simpleTextTokens("response"))
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, nil, log.Default(), nil)

	payload := marshalPayload(t, MessagePayload{Content: "hello"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	stop := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalStop,
	})
	err = h.HandleQueueMessage(ctx, stop)
	require.NoError(t, err)

	waitForAgentEviction(t, h, stream.ExternalID)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusCancelled, updated.Status)
}

func TestHarness_ShutdownCancelsAndDrainsRunningAgents(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()
	client := newBlockingClient()
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, nil, log.Default(), nil)

	payload := marshalPayload(t, MessagePayload{Content: "hello"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-client.started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err = h.Shutdown(shutdownCtx)
	require.NoError(t, err)

	select {
	case <-client.released:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the running LLM turn")
	}

	h.mu.RLock()
	_, exists := h.agents[stream.ExternalID]
	h.mu.RUnlock()
	require.False(t, exists)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusCancelled, updated.Status)
}

func TestHarness_HandleQueueMessage_stop_not_running(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	el := newMemoryEventLog()
	pub := newMockPublisher()
	clients := llm.NewClientRegistry()

	h := NewHarness(db, el, pub, clients, nil, log.Default(), nil)

	stop := marshalQueueEvent(t, QueueEvent{
		StreamID: "nonexistent-stream-id",
		Type:     enums.AgentEventSignalStop,
	})
	err := h.HandleQueueMessage(ctx, stop)
	require.Error(t, err)
	require.ErrorContains(t, err, "unable to get stream")
}

func TestHarness_HandleQueueMessage_invalid_json(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	h := NewHarness(db, nil, nil, nil, nil, log.Default(), nil)

	err := h.HandleQueueMessage(ctx, []byte(`not json`))
	require.Error(t, err)
	require.ErrorContains(t, err, "unable to unmarshal")
}

func TestHarness_HandleQueueMessage_missing_stream_id(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	h := NewHarness(db, nil, nil, nil, nil, log.Default(), nil)

	payload := marshalPayload(t, MessagePayload{Content: "hi"})
	msg := marshalQueueEvent(t, QueueEvent{
		Type:    enums.AgentEventSignalReceived,
		Payload: payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing stream_id")
}

func TestHarness_coldStartFailureMarksStreamFailed(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	pub := newMockPublisher()
	client := newStreamingMockClient(simpleTextTokens("unused"))
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, &failingListEventLog{memoryEventLog: newMemoryEventLog()}, pub, clients, nil, log.Default(), nil)

	payload := marshalPayload(t, MessagePayload{Content: "hello"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.Error(t, err)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusFailed, updated.Status)
}

func TestHarness_idle_timeout_evicts_agent_and_stop_is_idempotent(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()
	client := newStreamingMockClient(simpleTextTokens("hi"))
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, nil, log.Default(), nil).WithIdleTimeout(50 * time.Millisecond)

	payload := marshalPayload(t, MessagePayload{Content: "hello"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.NoError(t, err)

	waitForAgentEviction(t, h, stream.ExternalID)

	require.Eventually(t, func() bool {
		updated, err := agentstreams.Get(ctx, db, stream.ID)
		return err == nil && updated != nil && updated.Status == agentstreams.StatusIdle
	}, time.Second, 10*time.Millisecond)

	stop := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalStop,
	})
	err = h.HandleQueueMessage(ctx, stop)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		updated, err := agentstreams.Get(ctx, db, stream.ID)
		return err == nil && updated != nil && updated.Status == agentstreams.StatusCancelled
	}, time.Second, 10*time.Millisecond)
}

func TestHarness_cold_restart_after_idle(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()
	client := newStreamingMockClient(
		simpleTextTokens("first response"),
		simpleTextTokens("second response"),
	)
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	// Use a short timeout for the first phase (idle eviction)
	h := NewHarness(
		db,
		el,
		pub, clients,
		nil,
		log.Default(),
		nil,
	)
	h = h.WithIdleTimeout(50 * time.Millisecond)

	// Send first message
	payload1 := marshalPayload(t, MessagePayload{Content: "hello"})
	msg1 := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload1,
	})
	err := h.HandleQueueMessage(ctx, msg1)
	require.NoError(t, err)

	// Wait for idle timeout to evict the agent
	time.Sleep(300 * time.Millisecond)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusIdle, updated.Status)
	firstFence := updated.FenceToken

	// Use a longer timeout so the restarted agent is still alive when we check
	h.WithIdleTimeout(5 * time.Second)

	// Send second message — should cold-start with a new fence token
	payload2 := marshalPayload(t, MessagePayload{Content: "follow-up"})
	msg2 := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload2,
	})
	err = h.HandleQueueMessage(ctx, msg2)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	updated, err = agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusRunning, updated.Status)
	require.Greater(t, updated.FenceToken, firstFence)
}

func TestHarness_approval_suppresses_idle_timeout(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()

	client := newStreamingMockClient(approvalToolCallTokens())
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, []llm.Tool{approvalRequiredTool}, log.Default(), nil).
		WithIdleTimeout(50 * time.Millisecond)

	payload := marshalPayload(t, MessagePayload{Content: "do the gated thing"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.NoError(t, err)

	// Wait well beyond idle timeout
	time.Sleep(300 * time.Millisecond)

	h.mu.RLock()
	_, exists := h.agents[stream.ExternalID]
	h.mu.RUnlock()
	require.True(t, exists, "agent should not be evicted while approval is pending")

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusAwaitingApproval, updated.Status)
}

func TestHarness_approval_resolve_resumes_agent(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	el := newMemoryEventLog()
	pub := newMockPublisher()

	client := newStreamingMockClient(
		approvalToolCallTokens(),
		simpleTextTokens("approved and done"),
	)
	clients := llm.NewClientRegistry()
	clients.Register(anthropic.ClaudeSonnet45, client)

	h := NewHarness(db, el, pub, clients, []llm.Tool{approvalRequiredTool}, log.Default(), nil).
		WithIdleTimeout(50 * time.Millisecond)

	payload := marshalPayload(t, MessagePayload{Content: "do it"})
	msg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalReceived,
		Payload:  payload,
	})
	err := h.HandleQueueMessage(ctx, msg)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusAwaitingApproval, updated.Status)

	approval, err := agentapprovals.GetPendingByStreamID(ctx, db, stream.ID)
	require.NoError(t, err)
	require.NotNil(t, approval)

	approvalPayload := marshalPayload(t, ApprovalDecision{
		ApprovalID: approval.ExternalID,
		Approved:   true,
	})
	approvalMsg := marshalQueueEvent(t, QueueEvent{
		StreamID: stream.ExternalID,
		Type:     enums.AgentEventSignalApproval,
		Source:   enums.AgentEventSourceAPI,
		Payload:  approvalPayload,
	})
	err = h.HandleQueueMessage(ctx, approvalMsg)
	require.NoError(t, err)

	// Wait for turn to complete and idle timeout to fire
	time.Sleep(500 * time.Millisecond)

	h.mu.RLock()
	_, exists := h.agents[stream.ExternalID]
	h.mu.RUnlock()
	require.False(t, exists, "agent should be evicted after idle timeout")

	updated, err = agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusIdle, updated.Status)
}
