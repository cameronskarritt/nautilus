package signalintake

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"nautilus/internal/ai/agent"
	"nautilus/internal/ai/eventlog/pgeventlog"
	"nautilus/internal/database"
	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/database/outboxevents"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestSignalIntakeAcceptsSignals(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)
	organizationID := testutil.CreateTestOrg(t, db, "signal-intake", "Signal Intake")
	intake := New(db)

	created, err := intake.CreateStream(ctx, userID, organizationID, "hello")
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.NotNil(t, created.Stream.Title)
	require.Equal(t, "hello", *created.Stream.Title)

	event := claimSignal(t, ctx, db)
	require.Equal(t, created.Stream.ExternalID, event.StreamID)
	require.Equal(t, enums.AgentEventSignalReceived, event.Type)
	var message agent.MessagePayload
	require.NoError(t, json.Unmarshal(event.Payload, &message))
	require.Equal(t, created.ID, message.ID)
	require.Equal(t, "hello", message.Content)

	accepted, err := intake.Message(ctx, created.Stream.OrgID, created.Stream.ExternalID, "follow up")
	require.NoError(t, err)
	event = claimSignal(t, ctx, db)
	require.Equal(t, enums.AgentEventSignalReceived, event.Type)
	require.NoError(t, json.Unmarshal(event.Payload, &message))
	require.Equal(t, accepted.ID, message.ID)
	require.Equal(t, "follow up", message.Content)

	stopped, err := intake.Stop(ctx, created.Stream.OrgID, created.Stream.ExternalID)
	require.NoError(t, err)
	event = claimSignal(t, ctx, db)
	require.Equal(t, stopped.ID, eventID(t, ctx, db, created.Stream.ExternalID, enums.AgentEventSignalStop))
	require.Equal(t, enums.AgentEventSignalStop, event.Type)

	approval, err := agentapprovals.Create(ctx, db, created.Stream.ID, created.Stream.FenceToken, nil)
	require.NoError(t, err)
	updated, err := intake.ResolveApproval(ctx, ApprovalResolution{
		OrganizationID:  organizationID,
		ApprovalID:      approval.ExternalID,
		Approved:        true,
		ApproverID:      userID,
		Approver:        agent.Approver{Username: "approver"},
		ApproverMessage: optional.Empty[string](),
	})
	require.NoError(t, err)
	require.Equal(t, agentapprovals.StatusApproved, updated.Status)

	event = claimSignal(t, ctx, db)
	require.Equal(t, enums.AgentEventSignalApproval, event.Type)
	var decision agent.ApprovalDecision
	require.NoError(t, json.Unmarshal(event.Payload, &decision))
	require.Equal(t, approval.ExternalID, decision.ApprovalID)
	require.True(t, decision.Approved)

	_, err = intake.ResolveApproval(ctx, ApprovalResolution{
		OrganizationID: organizationID,
		ApprovalID:     approval.ExternalID,
		Approved:       true,
		ApproverID:     userID,
	})
	require.ErrorIs(t, err, agentapprovals.ErrNotPending)
}

func TestSignalIntakeRollsBackStateWhenOutboxEnqueueFails(t *testing.T) {
	db := testutil.SetupTestDBWithCommit(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)
	organizationID := testutil.CreateTestOrg(t, db, "signal-intake-rollback", "Signal Rollback")
	stream, err := agentstreams.Create(ctx, db, userID, organizationID)
	require.NoError(t, err)
	approval, err := agentapprovals.Create(ctx, db, stream.ID, stream.FenceToken, nil)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `DROP TABLE outbox_events`)
	require.NoError(t, err)

	_, err = New(db).CreateStream(ctx, userID, organizationID, "must roll back")
	require.Error(t, err)

	var streams int
	err = db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM agent_streams
			WHERE user_id = $1 AND org_id = $2 AND title = 'must roll back'`,
		userID,
		organizationID,
	).Scan(&streams)
	require.NoError(t, err)
	require.Equal(t, 0, streams)

	_, err = New(db).ResolveApproval(ctx, ApprovalResolution{
		OrganizationID: organizationID,
		ApprovalID:     approval.ExternalID,
		Approved:       true,
		ApproverID:     userID,
	})
	require.Error(t, err)

	unresolved, err := agentapprovals.Get(ctx, db, approval.ID)
	require.NoError(t, err)
	require.Equal(t, agentapprovals.StatusPending, unresolved.Status)
}

func TestSignalIntakeRejectsCrossOrganizationSignals(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	other := testutil.CreateTestStream(t, db)
	intake := New(db)

	accepted, err := intake.Message(t.Context(), other.OrgID, stream.ExternalID, "hidden")
	require.NoError(t, err)
	require.Nil(t, accepted)

	accepted, err = intake.Stop(t.Context(), other.OrgID, stream.ExternalID)
	require.NoError(t, err)
	require.Nil(t, accepted)

	approval, err := agentapprovals.Create(t.Context(), db, stream.ID, stream.FenceToken, nil)
	require.NoError(t, err)
	resolved, err := intake.ResolveApproval(t.Context(), ApprovalResolution{
		OrganizationID: other.OrgID,
		ApprovalID:     approval.ExternalID,
		Approved:       true,
		ApproverID:     other.UserID,
	})
	require.NoError(t, err)
	require.Nil(t, resolved)
}

func claimSignal(
	t *testing.T,
	ctx context.Context,
	db database.Database,
) agent.QueueEvent {
	t.Helper()

	outbox, err := outboxevents.Claim(ctx, db, string(enums.QueueAgentSignals), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, outbox)

	var event agent.QueueEvent
	require.NoError(t, json.Unmarshal(outbox.Payload, &event))
	return event
}

func eventID(
	t *testing.T,
	ctx context.Context,
	db database.Database,
	streamID string,
	eventType enums.AgentEventType,
) string {
	t.Helper()

	events, err := pgeventlog.New(db).List(ctx, streamID, 0)
	require.NoError(t, err)
	for _, event := range events {
		if event.Type == eventType && event.IdempotencyKey != nil {
			return *event.IdempotencyKey
		}
	}
	return ""
}
