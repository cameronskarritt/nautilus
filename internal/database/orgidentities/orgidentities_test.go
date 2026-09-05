package orgidentities_test

import (
	"context"
	"testing"

	"nautilus/internal/database/organizations"
	"nautilus/internal/database/orgidentities"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestEnsure(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	ownerID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "owner"})
	memberID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "member"})

	org, owner, err := orgidentities.Ensure(
		ctx,
		db,
		ownerID,
		enums.AuthProviderGitHub,
		"99",
		"github-99",
		"acme",
		organizations.RoleOwner,
	)
	require.NoError(t, err)
	require.NotNil(t, org)
	require.False(t, org.Personal)
	require.Equal(t, "acme", org.Name)
	require.Equal(t, organizations.RoleOwner, owner.Role)

	sameOrg, member, err := orgidentities.Ensure(
		ctx,
		db,
		memberID,
		enums.AuthProviderGitHub,
		"99",
		"github-99",
		"acme-renamed",
		organizations.RoleMember,
	)
	require.NoError(t, err)
	require.Equal(t, org.ID, sameOrg.ID)
	require.Equal(t, "acme-renamed", sameOrg.Name)
	require.Equal(t, organizations.RoleMember, member.Role)

	identity, err := orgidentities.GetByProvider(ctx, db, enums.AuthProviderGitHub, "99")
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, org.ID, identity.OrganizationID)
}

func TestEnsureUpdatesExistingMemberRole(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	_, member, err := orgidentities.Ensure(
		ctx,
		db,
		userID,
		enums.AuthProviderGitHub,
		"100",
		"github-100",
		"example",
		organizations.RoleMember,
	)
	require.NoError(t, err)
	require.Equal(t, organizations.RoleMember, member.Role)

	_, member, err = orgidentities.Ensure(
		ctx,
		db,
		userID,
		enums.AuthProviderGitHub,
		"100",
		"github-100",
		"example",
		organizations.RoleOwner,
	)
	require.NoError(t, err)
	require.Equal(t, organizations.RoleOwner, member.Role)

	err = organizations.DeleteMember(ctx, db, member.ID)
	require.NoError(t, err)

	_, restored, err := orgidentities.Ensure(
		ctx,
		db,
		userID,
		enums.AuthProviderGitHub,
		"100",
		"github-100",
		"example",
		organizations.RoleMember,
	)
	require.NoError(t, err)
	require.Equal(t, member.ID, restored.ID)
	require.Equal(t, organizations.RoleMember, restored.Role)
}
