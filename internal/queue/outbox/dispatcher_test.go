package outbox

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"nautilus/internal/database/outboxevents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type recordingPublisher struct {
	topic   enums.Queue
	payload json.RawMessage
	err     error
}

func (p *recordingPublisher) Publish(_ context.Context, topic enums.Queue, payload any) (string, error) {
	p.topic = topic
	p.payload = payload.(json.RawMessage)
	return "message-1", p.err
}

func TestDispatcherDispatch(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	organizationID := testutil.CreateTestOrg(t, db, "outbox-dispatcher", "Outbox Dispatcher")
	topic := enums.Queue("test-events")
	payload := map[string]any{"event": "completed"}

	event, err := outboxevents.Enqueue(
		ctx,
		db,
		organizationID,
		string(topic),
		"aggregate-1",
		"event-1",
		payload,
		time.Time{},
	)
	require.NoError(t, err)

	publisher := new(recordingPublisher)
	dispatcher := NewDispatcher(db, publisher)
	dispatched, err := dispatcher.dispatch(ctx, topic)
	require.NoError(t, err)
	require.True(t, dispatched)
	require.Equal(t, topic, publisher.topic)

	var published map[string]any
	require.NoError(t, json.Unmarshal(publisher.payload, &published))
	require.Equal(t, "completed", published["event"])

	processed, err := outboxevents.Get(ctx, db, organizationID, event.ID)
	require.NoError(t, err)
	require.True(t, processed.ProcessedAt.Set)
	require.False(t, processed.LeaseToken.Set)
}

func TestDispatcherRetainsEventWhenPublishFails(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	organizationID := testutil.CreateTestOrg(t, db, "outbox-publish-failure", "Outbox Failure")
	topic := enums.Queue("test-events")

	event, err := outboxevents.Enqueue(
		ctx,
		db,
		organizationID,
		string(topic),
		"aggregate-1",
		"event-1",
		map[string]any{"event": "completed"},
		time.Time{},
	)
	require.NoError(t, err)

	dispatcher := NewDispatcher(db, &recordingPublisher{err: errors.New("publish failed")})
	dispatched, err := dispatcher.dispatch(ctx, topic)
	require.Error(t, err)
	require.False(t, dispatched)

	retained, err := outboxevents.Get(ctx, db, organizationID, event.ID)
	require.NoError(t, err)
	require.False(t, retained.ProcessedAt.Set)
	require.True(t, retained.LeaseToken.Set)
}
