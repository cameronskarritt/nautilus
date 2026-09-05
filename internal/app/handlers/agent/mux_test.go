package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"nautilus/internal/ai/agent"
	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/eventlog/pgeventlog"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/outboxevents"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/pubsub"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type orderedSubscribePubSub struct {
	subscribed atomic.Bool
	sub        *orderedSubscription
}

type orderedSubscription struct {
	messages chan []byte
	closed   atomic.Bool
}

func newOrderedSubscribePubSub() *orderedSubscribePubSub {
	ch := make(chan []byte)
	close(ch)
	return &orderedSubscribePubSub{sub: &orderedSubscription{messages: ch}}
}

func (p *orderedSubscribePubSub) Publish(context.Context, string, any) error {
	return nil
}

func (p *orderedSubscribePubSub) Subscribe(context.Context, string) (pubsub.Subscription, error) {
	p.subscribed.Store(true)
	return p.sub, nil
}

func (s *orderedSubscription) Messages() <-chan []byte {
	return s.messages
}

func (s *orderedSubscription) Close() error {
	s.closed.Store(true)
	return nil
}

type orderedSubscribeEventLog struct {
	subscribed *atomic.Bool
}

type failingPubSub struct{}

func (failingPubSub) Publish(context.Context, string, any) error {
	return nil
}

func (failingPubSub) Subscribe(context.Context, string) (pubsub.Subscription, error) {
	return nil, errors.New("private subscription failure")
}

func (l *orderedSubscribeEventLog) Append(context.Context, string, enums.AgentEventType, enums.AgentEventSource, any, eventlog.Tokens) (*eventlog.Event, error) {
	return nil, nil
}

func (l *orderedSubscribeEventLog) List(_ context.Context, streamID string, _ int64) ([]*eventlog.Event, error) {
	if !l.subscribed.Load() {
		return nil, errors.New("listed before subscribe")
	}

	return []*eventlog.Event{
		{
			ID:        "event-1",
			StreamID:  streamID,
			Sequence:  1,
			Type:      enums.AgentEventMessage,
			Source:    enums.AgentEventSourceAPI,
			Payload:   json.RawMessage(`{"content":"hello"}`),
			CreatedAt: time.Now(),
		},
	}, nil
}

func TestMux_CreateStream_recordsInitialMessage(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "agent-create-stream-test", "Agent Test")

	user, err := users.Get(ctx, db, userID)
	require.NoError(t, err)
	org, err := organizations.Get(ctx, db, orgID)
	require.NoError(t, err)

	m := NewMux(db, nil)

	body := bytes.NewBufferString(`{"message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/streams", body)
	req = req.WithContext(organizations.WithContext(users.WithContext(req.Context(), user), org))
	rec := httptest.NewRecorder()

	m.CreateStream(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var response struct {
		StreamID string `json:"stream_id"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.NotEmpty(t, response.StreamID)

	events, err := pgeventlog.New(db).List(ctx, response.StreamID, 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, enums.AgentEventSignalReceived, events[0].Type)
	require.Equal(t, enums.AgentEventMessage, events[1].Type)

	var recorded agent.MessagePayload
	err = json.Unmarshal(events[1].Payload, &recorded)
	require.NoError(t, err)
	require.NotEmpty(t, recorded.ID)
	require.Equal(t, "hello", recorded.Content)

	outbox, err := outboxevents.Claim(ctx, db, string(enums.QueueAgentSignals), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, outbox)

	var queued agent.QueueEvent
	err = json.Unmarshal(outbox.Payload, &queued)
	require.NoError(t, err)
	var queuedMessage agent.MessagePayload
	err = json.Unmarshal(queued.Payload, &queuedMessage)
	require.NoError(t, err)
	require.Equal(t, recorded.ID, queuedMessage.ID)
	require.Equal(t, recorded.Content, queuedMessage.Content)
}

func TestMux_StreamEventsSubscribesBeforeCatchup(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	org, err := organizations.Get(t.Context(), db, stream.OrgID)
	require.NoError(t, err)

	pubsub := newOrderedSubscribePubSub()
	m := NewMux(db, pubsub)
	m.eventLog = &orderedSubscribeEventLog{subscribed: &pubsub.subscribed}

	router := mux.New()
	m.Mount(router, "/agent")

	req := httptest.NewRequest(http.MethodGet, "/agent/streams/"+stream.ExternalID+"/events", nil)
	req = req.WithContext(organizations.WithContext(req.Context(), org))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	require.Contains(t, body, "event: event")
	require.NotContains(t, body, "listed before subscribe")
	require.True(t, pubsub.sub.closed.Load())
}

func TestMux_StreamEventsEnforcesOrganizationScope(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	other := testutil.CreateTestStream(t, db)
	org, err := organizations.Get(t.Context(), db, other.OrgID)
	require.NoError(t, err)

	router := mux.New()
	NewMux(db, nil).Mount(router, "/agent")
	req := httptest.NewRequest(http.MethodGet, "/agent/streams/"+stream.ExternalID+"/events", nil)
	req = req.WithContext(organizations.WithContext(req.Context(), org))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), errors.ErrorCodeAGENT03)
}

func TestMux_StreamEventsHidesInternalErrors(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	org, err := organizations.Get(t.Context(), db, stream.OrgID)
	require.NoError(t, err)

	router := mux.New()
	NewMux(db, failingPubSub{}).Mount(router, "/agent")
	req := httptest.NewRequest(http.MethodGet, "/agent/streams/"+stream.ExternalID+"/events", nil)
	req = req.WithContext(organizations.WithContext(req.Context(), org))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Contains(t, rec.Body.String(), errors.ErrorCodeAGENT05)
	require.NotContains(t, rec.Body.String(), "private subscription failure")
}
