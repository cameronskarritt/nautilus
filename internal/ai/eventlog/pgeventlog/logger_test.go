package pgeventlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/eventlog/pgeventlog"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestLogger(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)

	logger := pgeventlog.New(db)
	created, err := logger.Append(ctx, stream.ExternalID, enums.AgentEventMessage, enums.AgentEventSourceAPI, map[string]string{
		"content": "hello",
	}, eventlog.Tokens{Fence: token})
	require.NoError(t, err)
	require.Equal(t, stream.ExternalID, created.StreamID)
	require.Equal(t, int64(1), created.Sequence)
	require.Equal(t, enums.AgentEventMessage, created.Type)

	var payload struct {
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(created.Payload, &payload))
	require.Equal(t, "hello", payload.Content)

	_, err = logger.Append(ctx, stream.ExternalID, enums.AgentEventTurnCompleted, enums.AgentEventSourceAgent, nil, eventlog.Tokens{Fence: token})
	require.NoError(t, err)

	events, err := logger.List(ctx, stream.ExternalID, 1)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(2), events[0].Sequence)
	require.Equal(t, enums.AgentEventTurnCompleted, events[0].Type)

	_, err = logger.Append(ctx, stream.ExternalID, enums.AgentEventMessage, enums.AgentEventSourceAPI, nil, eventlog.Tokens{Fence: token - 1})
	require.ErrorIs(t, err, eventlog.ErrFenceViolation)
}
