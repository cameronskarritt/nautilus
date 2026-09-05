package agentevents_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"nautilus/internal/database/agentevents"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

const testSource = enums.AgentEventSourceAgent

func TestAppend_assignsContiguousSequences(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	tests := []struct {
		name    string
		typ     enums.AgentEventType
		payload any
	}{
		{
			name: "first event starts sequence",
			typ:  enums.AgentEventSignalReceived,
			payload: map[string]any{
				"content": "hello",
			},
		},
		{
			name: "second event increments sequence",
			typ:  enums.AgentEventLLMText,
			payload: map[string]any{
				"text": "chunk",
			},
		},
		{
			name:    "third event increments sequence",
			typ:     enums.AgentEventTurnCompleted,
			payload: nil,
		},
	}

	for i, tt := range tests {
		event, err := agentevents.Append(ctx, db, stream.ID, tt.typ, testSource, tt.payload, agentevents.AppendOptions{Fence: token})
		require.NoError(t, err, tt.name)
		require.NotNil(t, event, tt.name)
		require.NotZero(t, event.ID, tt.name)
		require.Equal(t, stream.ID, event.StreamID, tt.name)
		require.Equal(t, int64(i+1), event.Sequence, tt.name)
		require.Equal(t, tt.typ, event.Type, tt.name)
		require.Equal(t, testSource, event.Source, tt.name)
	}
}

func TestAppend_concurrentWritersUseContiguousSequences(t *testing.T) {
	db := testutil.SetupTestDBWithCommit(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	const count = 50
	var wg sync.WaitGroup
	errs := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := agentevents.Append(ctx, db, stream.ID, enums.AgentEventMessage, testSource, map[string]any{
				"content": fmt.Sprintf("message-%d", i),
			}, agentevents.AppendOptions{
				Fence:       token,
				Idempotency: fmt.Sprintf("msg-%d", i),
			})
			errs <- err
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	events, err := agentevents.ListByStream(ctx, db, stream.ID, 0)
	require.NoError(t, err)
	require.Len(t, events, count)
	for i, event := range events {
		require.Equal(t, int64(i+1), event.Sequence)
	}
}

func TestAppend_idempotency_returnsExisting(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	first, err := agentevents.Append(ctx, db, stream.ID, enums.AgentEventMessage, testSource, map[string]any{
		"content": "hello",
	}, agentevents.AppendOptions{Fence: token, Idempotency: "msg-1"})
	require.NoError(t, err)
	require.NotNil(t, first.IdempotencyKey)
	require.Equal(t, "msg-1", *first.IdempotencyKey)

	second, err := agentevents.Append(ctx, db, stream.ID, enums.AgentEventMessage, testSource, map[string]any{
		"content": "hello again",
	}, agentevents.AppendOptions{Fence: token, Idempotency: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.Sequence, second.Sequence)

	next, err := agentevents.Append(ctx, db, stream.ID, enums.AgentEventTurnStarted, testSource, nil, agentevents.AppendOptions{Fence: token})
	require.NoError(t, err)
	require.Equal(t, first.Sequence+1, next.Sequence)
}

func TestAppend_rejectsInvalidFence(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	oldToken, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	_, err = agentevents.Append(ctx, db, stream.ID, enums.AgentEventTurnStarted, testSource, nil, agentevents.AppendOptions{Fence: oldToken})
	require.NoError(t, err)

	newToken, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Greater(t, newToken, oldToken)

	tests := []struct {
		name  string
		fence int64
	}{
		{name: "wrong token", fence: 0},
		{name: "stale token", fence: oldToken},
	}

	for _, tt := range tests {
		_, err := agentevents.Append(ctx, db, stream.ID, enums.AgentEventLLMText, testSource, map[string]any{
			"text": "from invalid writer",
		}, agentevents.AppendOptions{Fence: tt.fence})
		require.ErrorIs(t, err, agentevents.ErrFenceViolation, tt.name)
	}

	event, err := agentevents.Append(ctx, db, stream.ID, enums.AgentEventLLMText, testSource, map[string]any{
		"text": "from current writer",
	}, agentevents.AppendOptions{Fence: newToken})
	require.NoError(t, err)
	require.Equal(t, int64(2), event.Sequence)
}

func TestGet_missing(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	fetched, err := agentevents.Get(ctx, db, 99999)
	require.NoError(t, err)
	require.Nil(t, fetched)
}

func TestListByStream(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	eventTypes := []enums.AgentEventType{
		enums.AgentEventSignalReceived,
		enums.AgentEventTurnStarted,
		enums.AgentEventLLMText,
		enums.AgentEventLLMResponse,
		enums.AgentEventMessage,
	}

	for _, eventType := range eventTypes {
		_, err := agentevents.Append(ctx, db, stream.ID, eventType, testSource, nil, agentevents.AppendOptions{Fence: token})
		require.NoError(t, err)
	}

	events, err := agentevents.ListByStream(ctx, db, stream.ID, 0)
	require.NoError(t, err)
	require.Len(t, events, len(eventTypes))

	for i, event := range events {
		require.Equal(t, int64(i+1), event.Sequence)
		require.Equal(t, eventTypes[i], event.Type)
	}

	events, err = agentevents.ListByStream(ctx, db, stream.ID, 3)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, int64(4), events[0].Sequence)
	require.Equal(t, eventTypes[3], events[0].Type)
	require.Equal(t, int64(5), events[1].Sequence)
	require.Equal(t, eventTypes[4], events[1].Type)
}

func TestListByType(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	_, err = agentevents.Append(ctx, db, stream.ID, enums.AgentEventSignalReceived, testSource, nil, agentevents.AppendOptions{Fence: token})
	require.NoError(t, err)
	_, err = agentevents.Append(ctx, db, stream.ID, enums.AgentEventLLMText, testSource, nil, agentevents.AppendOptions{Fence: token})
	require.NoError(t, err)
	_, err = agentevents.Append(ctx, db, stream.ID, enums.AgentEventLLMText, testSource, nil, agentevents.AppendOptions{Fence: token})
	require.NoError(t, err)
	_, err = agentevents.Append(ctx, db, stream.ID, enums.AgentEventLLMResponse, testSource, nil, agentevents.AppendOptions{Fence: token})
	require.NoError(t, err)

	events, err := agentevents.ListByType(ctx, db, enums.AgentEventLLMText)
	require.NoError(t, err)
	require.Len(t, events, 2)
	for _, event := range events {
		require.Equal(t, enums.AgentEventLLMText, event.Type)
	}
}
