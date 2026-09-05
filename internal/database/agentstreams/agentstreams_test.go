package agentstreams_test

import (
	"context"
	"testing"

	"nautilus/internal/database/agentstreams"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCreate(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "agentstreams-create", "Test Org")

	stream, err := agentstreams.Create(ctx, db, userID, orgID)
	require.NoError(t, err)
	require.NotNil(t, stream)
	require.NotZero(t, stream.ID)
	require.NotEmpty(t, stream.ExternalID)
	require.Equal(t, userID, stream.UserID)
	require.Equal(t, orgID, stream.OrgID)
	require.Equal(t, agentstreams.StatusPending, stream.Status)
	require.Equal(t, int64(0), stream.FenceToken)
	require.Nil(t, stream.Title)
	require.False(t, stream.CreatedAt.IsZero())
	require.False(t, stream.UpdatedAt.IsZero())
}

func TestGet_missing(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	fetched, err := agentstreams.Get(ctx, db, 99999)
	require.NoError(t, err)
	require.Nil(t, fetched)
}

func TestGetByExternalID(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)

	fetched, err := agentstreams.GetByExternalID(ctx, db, stream.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, stream.ID, fetched.ID)
	require.Equal(t, stream.ExternalID, fetched.ExternalID)
}

func TestGetByExternalID_missing(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	fetched, err := agentstreams.GetByExternalID(ctx, db, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	require.Nil(t, fetched)
}

func TestGetByExternalIDForOrganization(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	stream := testutil.CreateTestStream(t, db)
	other := testutil.CreateTestStream(t, db)

	fetched, err := agentstreams.GetByExternalIDForOrganization(
		t.Context(),
		db,
		stream.OrgID,
		stream.ExternalID,
	)
	require.NoError(t, err)
	require.Equal(t, stream.ID, fetched.ID)

	fetched, err = agentstreams.GetByExternalIDForOrganization(
		t.Context(),
		db,
		other.OrgID,
		stream.ExternalID,
	)
	require.NoError(t, err)
	require.Nil(t, fetched)
}

func TestProjectStatus(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	require.Equal(t, agentstreams.StatusPending, stream.Status)

	err := agentstreams.ProjectStatus(ctx, db, stream.ID, stream.FenceToken, agentstreams.StatusIdle)
	require.NoError(t, err)

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusIdle, updated.Status)
	require.True(t, updated.UpdatedAt.After(stream.UpdatedAt) || updated.UpdatedAt.Equal(stream.UpdatedAt))

	_, err = agentstreams.AcquireFence(ctx, db, stream.ID)
	require.NoError(t, err)
	err = agentstreams.ProjectStatus(ctx, db, stream.ID, stream.FenceToken, agentstreams.StatusIdle)
	require.ErrorIs(t, err, agentstreams.ErrFenceViolation)
}

func TestAcquireFence(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	stream := testutil.CreateTestStream(t, db)
	require.Equal(t, int64(0), stream.FenceToken)
	require.Equal(t, agentstreams.StatusPending, stream.Status)

	for i := int64(1); i <= 5; i++ {
		token, err := agentstreams.AcquireFence(ctx, db, stream.ID)
		require.NoError(t, err)
		require.Equal(t, i, token)
	}

	updated, err := agentstreams.Get(ctx, db, stream.ID)
	require.NoError(t, err)
	require.Equal(t, agentstreams.StatusRunning, updated.Status)
	require.Equal(t, int64(5), updated.FenceToken)
}

func TestList(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	s1 := testutil.CreateTestStream(t, db)
	s2, err := agentstreams.Create(context.Background(), db, s1.UserID, s1.OrgID)
	require.NoError(t, err)
	other := testutil.CreateTestStream(t, db)

	page, err := agentstreams.List(context.Background(), db, s1.OrgID, pagination.Params{Limit: 50})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(page.Data), 2)

	var found1, found2 bool
	for _, s := range page.Data {
		if s.ID == s1.ID {
			found1 = true
		}
		if s.ID == s2.ID {
			found2 = true
		}
	}
	require.True(t, found1)
	require.True(t, found2)
	for _, stream := range page.Data {
		require.NotEqual(t, other.ID, stream.ID)
	}
}
