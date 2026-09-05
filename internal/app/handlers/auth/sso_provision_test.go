package auth

import (
	"context"
	"testing"

	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/sso"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestSSOMuxProvisionOrganizationUser(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	mux := &SSOMux{db: db}

	owner, err := mux.provision(ctx, enums.AuthProviderGitHub, &sso.UserInfo{
		ProviderID: "42",
		Email:      "owner@example.com",
		Organization: &sso.OrganizationInfo{
			ProviderID: "99",
			Slug:       "acme",
			Name:       "acme",
			Admin:      true,
		},
	})
	require.NoError(t, err)
	require.True(t, owner.created)
	require.Equal(t, organizations.RoleOwner, owner.member.Role)

	ownerOrg, err := organizations.Get(ctx, db, owner.member.OrganizationID)
	require.NoError(t, err)
	require.NotNil(t, ownerOrg)
	require.False(t, ownerOrg.Personal)
	require.Equal(t, "acme-99", ownerOrg.Slug)

	personalOrg, err := organizations.GetPersonalByUserID(ctx, db, owner.user.ID)
	require.NoError(t, err)
	require.Nil(t, personalOrg)

	member, err := mux.provision(ctx, enums.AuthProviderGitHub, &sso.UserInfo{
		ProviderID: "43",
		Email:      "member@example.com",
		Organization: &sso.OrganizationInfo{
			ProviderID: "99",
			Slug:       "acme",
			Name:       "acme",
		},
	})
	require.NoError(t, err)
	require.True(t, member.created)
	require.Equal(t, organizations.RoleMember, member.member.Role)
	require.Equal(t, owner.member.OrganizationID, member.member.OrganizationID)

	again, err := mux.provision(ctx, enums.AuthProviderGitHub, &sso.UserInfo{
		ProviderID: "43",
		Email:      "member@example.com",
		Organization: &sso.OrganizationInfo{
			ProviderID: "99",
			Slug:       "acme",
			Name:       "acme",
		},
	})
	require.NoError(t, err)
	require.False(t, again.created)
	require.Equal(t, member.member.ID, again.member.ID)
}
