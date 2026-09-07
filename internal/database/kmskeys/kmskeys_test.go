package kmskeys_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/kmskeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestKeys(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	first := createOrganization(t, db, "first")
	second := createOrganization(t, db, "second")

	key, err := kmskeys.GetOrganization(ctx, db, first.ID)
	require.NoError(t, err)
	require.Nil(t, key)
	key, err = kmskeys.GetUser(ctx, db)
	require.NoError(t, err)
	require.Nil(t, key)

	input := []byte("wrapped first key")
	key, err = kmskeys.CreateOrganization(ctx, db, first.ID, "first-provider-key", input)
	require.NoError(t, err)
	require.NotNil(t, key)
	require.Equal(t, optional.Set(first.ID), key.OrganizationID)
	require.Equal(t, "first-provider-key", key.ProviderKeyID)
	require.Equal(t, input, key.Ciphertext)
	require.NotZero(t, key.ID)
	require.False(t, key.CreatedAt.IsZero())

	input[0] = '!'
	require.Equal(t, []byte("wrapped first key"), key.Ciphertext)
	stored, err := kmskeys.GetOrganization(ctx, db, first.ID)
	require.NoError(t, err)
	require.Equal(t, key, stored)
	key.Ciphertext[0] = '!'
	require.Equal(t, []byte("wrapped first key"), stored.Ciphertext)

	again, err := kmskeys.CreateOrganization(ctx, db, first.ID, "replacement-provider-key", []byte("replacement"))
	require.NoError(t, err)
	require.Equal(t, stored, again)

	other, err := kmskeys.CreateOrganization(ctx, db, second.ID, "second-provider-key", []byte("wrapped second key"))
	require.NoError(t, err)
	require.NotEqual(t, stored.ID, other.ID)
	require.Equal(t, optional.Set(second.ID), other.OrganizationID)

	user, err := kmskeys.CreateUser(ctx, db, "user-provider-key", []byte("wrapped user key"))
	require.NoError(t, err)
	require.False(t, user.OrganizationID.Set)
	again, err = kmskeys.CreateUser(ctx, db, "replacement-user-provider", []byte("replacement"))
	require.NoError(t, err)
	require.Equal(t, user, again)

	for _, record := range []*kmskeys.Key{stored, other, user} {
		encoded, err := json.Marshal(record)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(encoded))
	}
}

func TestOrganizationUnavailable(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing", "deleted before creation", "deleted after creation"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := t.Context()
			org := createOrganization(t, db, name)
			if name == "missing" {
				_, err := db.Exec(ctx, "DELETE FROM organizations WHERE id = $1", org.ID)
				require.NoError(t, err)
			} else {
				if name == "deleted after creation" {
					_, err := kmskeys.CreateOrganization(ctx, db, org.ID, "retained-provider", []byte("retained"))
					require.NoError(t, err)
				}
				require.NoError(t, organizations.Delete(ctx, db, org.ID))
			}
			key, err := kmskeys.GetOrganization(ctx, db, org.ID)
			require.NoError(t, err)
			require.Nil(t, key)
			key, err = kmskeys.CreateOrganization(ctx, db, org.ID, "new-provider", []byte("new"))
			require.NoError(t, err)
			require.Nil(t, key)
			var count int
			require.NoError(t, db.QueryRow(ctx, "SELECT count(*) FROM kms_keys WHERE organization_id = $1", org.ID).Scan(&count))
			if name == "deleted after creation" {
				require.Equal(t, 1, count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

func TestKeyValidation(t *testing.T) {
	t.Parallel()
	for _, orgID := range []int{0, -1} {
		t.Run(fmt.Sprintf("organization %d", orgID), func(t *testing.T) {
			t.Parallel()
			_, err := kmskeys.GetOrganization(t.Context(), nil, orgID)
			require.Error(t, err)
			_, err = kmskeys.CreateOrganization(t.Context(), nil, orgID, "provider", []byte("wrapped"))
			require.Error(t, err)
		})
	}
	for _, tt := range []struct {
		name       string
		provider   string
		ciphertext []byte
	}{
		{name: "empty provider", ciphertext: []byte("wrapped")},
		{name: "blank provider", provider: " \n\t", ciphertext: []byte("wrapped")},
		{name: "nil ciphertext", provider: "provider"},
		{name: "empty ciphertext", provider: "provider", ciphertext: []byte{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := kmskeys.CreateOrganization(t.Context(), nil, 1, tt.provider, tt.ciphertext)
			require.Error(t, err)
			_, err = kmskeys.CreateUser(t.Context(), nil, tt.provider, tt.ciphertext)
			require.Error(t, err)
		})
	}
}

func TestProviderKeyScopeUniqueness(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"organization", "user"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			first := createOrganization(t, db, "first")
			_, err := kmskeys.CreateOrganization(t.Context(), db, first.ID, "same-provider", []byte("wrapped"))
			require.NoError(t, err)
			if scope == "organization" {
				second := createOrganization(t, db, "second")
				_, err = kmskeys.CreateOrganization(t.Context(), db, second.ID, "same-provider", []byte("other"))
			} else {
				_, err = kmskeys.CreateUser(t.Context(), db, "same-provider", []byte("other"))
			}
			require.Error(t, err)
			key, err := kmskeys.GetOrganization(t.Context(), db, first.ID)
			require.NoError(t, err)
			require.NotNil(t, key)
			require.Equal(t, []byte("wrapped"), key.Ciphertext)
		})
	}
}

func TestConcurrentCreate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		scope string
		same  bool
	}{
		{scope: "organization"},
		{scope: "organization", same: true},
		{scope: "user"},
		{scope: "user", same: true},
	} {
		t.Run(fmt.Sprintf("%s/same-provider=%t", tt.scope, tt.same), func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDBWithCommit(t)
			org := createOrganization(t, db, "concurrent")
			const n = 8
			keys := make([]*kmskeys.Key, n)
			errs := make([]error, n)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range n {
				wg.Go(func() {
					<-start
					provider := fmt.Sprintf("provider-%d", i)
					if tt.same {
						provider = "same-provider"
					}
					ciphertext := []byte(fmt.Sprintf("wrapped-%d", i))
					if tt.scope == "organization" {
						keys[i], errs[i] = kmskeys.CreateOrganization(t.Context(), db, org.ID, provider, ciphertext)
					} else {
						keys[i], errs[i] = kmskeys.CreateUser(t.Context(), db, provider, ciphertext)
					}
				})
			}
			close(start)
			wg.Wait()
			for i := range n {
				require.NoError(t, errs[i])
				require.NotNil(t, keys[i])
				require.Equal(t, keys[0], keys[i])
			}
			var count int
			require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM kms_keys").Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func createOrganization(t *testing.T, db database.Database, suffix string) *organizations.Organization {
	t.Helper()
	org, err := organizations.Create(t.Context(), db, t.Name()+suffix, suffix, false, optional.Empty[organizations.Settings]())
	require.NoError(t, err)
	return org
}
