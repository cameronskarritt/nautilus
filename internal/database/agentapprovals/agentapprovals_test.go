package agentapprovals_test

import (
	"testing"

	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCreateRejectsStaleFence(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	_, err := agentstreams.AcquireFence(t.Context(), db, stream.ID)
	require.NoError(t, err)

	approval, err := agentapprovals.Create(t.Context(), db, stream.ID, stream.FenceToken, nil)
	require.ErrorIs(t, err, agentstreams.ErrFenceViolation)
	require.Nil(t, approval)

	pending, err := agentapprovals.GetPendingByStreamID(t.Context(), db, stream.ID)
	require.NoError(t, err)
	require.Nil(t, pending)
}

func TestOrganizationScope(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	other := testutil.CreateTestStream(t, db)
	approval, err := agentapprovals.Create(t.Context(), db, stream.ID, stream.FenceToken, nil)
	require.NoError(t, err)

	fetched, err := agentapprovals.GetByExternalIDForOrganization(
		t.Context(),
		db,
		stream.OrgID,
		approval.ExternalID,
	)
	require.NoError(t, err)
	require.Equal(t, approval.ID, fetched.ID)

	fetched, err = agentapprovals.GetByExternalIDForOrganization(
		t.Context(),
		db,
		other.OrgID,
		approval.ExternalID,
	)
	require.NoError(t, err)
	require.Nil(t, fetched)

	pending, err := agentapprovals.ListPending(t.Context(), db, stream.OrgID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	pending, err = agentapprovals.ListPending(t.Context(), db, other.OrgID)
	require.NoError(t, err)
	require.Empty(t, pending)
}
