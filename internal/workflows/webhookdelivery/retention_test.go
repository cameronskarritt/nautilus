package webhookdelivery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database/webhooks"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRetentionWorkflow(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	RegisterRetention(env, Activities{})
	start := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.OnActivity(pruneName, mock.Anything, start.Add(-webhooks.Retention)).Return(0, unavailable()).Once()
	env.OnActivity(pruneName, mock.Anything, start.Add(-webhooks.Retention)).Return(1000, nil).Once()
	env.ExecuteWorkflow(RetentionName)
	var next *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &next)
	require.Equal(t, RetentionName, next.WorkflowType.Name)
	require.True(t, env.Now().Sub(start) >= 24*time.Hour)
	env.AssertExpectations(t)
}

func TestRetentionWorkflowCancellation(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	RegisterRetention(env, Activities{})
	env.OnActivity(pruneName, mock.Anything, mock.Anything).Return(0, nil).Once()
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Hour)
	env.ExecuteWorkflow(RetentionName)
	require.True(t, temporal.IsCanceledError(env.GetWorkflowError()))
	env.AssertExpectations(t)
}

func TestStartRetention(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "start or use existing"},
		{name: "unavailable", err: serviceerror.NewUnavailable("unavailable")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := mocks.NewClient(t)
			c.On("ExecuteWorkflow", mock.MatchedBy(func(ctx context.Context) bool {
				deadline, ok := ctx.Deadline()
				return ok && time.Until(deadline) > 0 && time.Until(deadline) <= 10*time.Second
			}), client.StartWorkflowOptions{
				ID:                       "webhook-retention",
				TaskQueue:                queueName,
				WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
				WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			}, RetentionName).Return(nil, tt.err).Once()
			err := StartRetention(t.Context(), c)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRetentionActivityIsBounded(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "retention", "Retention")
	cutoff := time.Now().Add(-webhooks.Retention)
	_, err := db.Exec(ctx, `INSERT INTO events(organization_id, type, schema_version, idempotency_key, payload, occurred_at, created_at)
  SELECT $1, 'webhook.test', 1, 'retention-' || n, '{}', $2, $2 FROM generate_series(1,10001) n`, orgID, cutoff.Add(-time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO events(organization_id, type, schema_version, idempotency_key, payload, occurred_at)
  VALUES ($1, 'webhook.test', 1, 'fresh', '{}', CURRENT_TIMESTAMP)`, orgID)
	require.NoError(t, err)
	a := Activities{DB: db}
	count, err := a.Prune(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 10000, count)
	count, err = a.Prune(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var key string
	require.NoError(t, db.QueryRow(ctx, `SELECT idempotency_key FROM events WHERE organization_id = $1`, orgID).Scan(&key))
	require.Equal(t, "fresh", key)
}
