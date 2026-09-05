package apikeys_test

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCreateReturnsTokenOnceAndStoresHash(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	organizationID, userID := apiKeyOwner(t, db, "create")

	key, token, err := apikeys.Create(t.Context(), db, organizationID, userID, &apikeys.CreateOptions{
		Name:   " Production ",
		Scopes: []apikeys.Scope{apikeys.ScopeWrite, apikeys.ScopeRead, apikeys.ScopeWrite},
	})
	require.NoError(t, err)
	require.Equal(t, "Production", key.Name)
	require.Equal(t, []apikeys.Scope{apikeys.ScopeRead, apikeys.ScopeWrite}, key.Scopes)
	require.True(t, strings.HasPrefix(token, "nautilus_"))
	require.Equal(t, token[:15], key.Prefix)
	require.NotEmpty(t, key.ExternalID)

	var hash []byte
	require.NoError(t, db.QueryRow(t.Context(), `
		SELECT token_hash
		FROM api_keys
		WHERE id = $1;
	`, key.ID).Scan(&hash))
	wantHash := sha256.Sum256([]byte(token))
	require.Equal(t, wantHash[:], hash)

	data, err := json.Marshal(key)
	require.NoError(t, err)
	require.NotContains(t, string(data), token)
	require.NotContains(t, string(data), "organization_id")
	require.NotContains(t, string(data), "created_by")
}

func TestListIsOrganizationScopedAndNewestFirst(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	firstOrganizationID, userID := apiKeyOwner(t, db, "list-first")
	secondOrganizationID := testutil.CreateTestOrg(t, db, "api-key-list-second", "API Key List Second")
	first, _, err := apikeys.Create(t.Context(), db, firstOrganizationID, userID, keyOptions("First"))
	require.NoError(t, err)
	second, _, err := apikeys.Create(t.Context(), db, firstOrganizationID, userID, keyOptions("Second"))
	require.NoError(t, err)
	_, _, err = apikeys.Create(t.Context(), db, secondOrganizationID, userID, keyOptions("Other"))
	require.NoError(t, err)

	keys, err := apikeys.List(t.Context(), db, firstOrganizationID)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, second.ExternalID, keys[0].ExternalID)
	require.Equal(t, first.ExternalID, keys[1].ExternalID)

	keys, err = apikeys.List(t.Context(), db, secondOrganizationID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.NotEqual(t, first.ExternalID, keys[0].ExternalID)
}

func TestRevokeIsOrganizationScopedAndDisablesAuthentication(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	organizationID, userID := apiKeyOwner(t, db, "revoke")
	otherOrganizationID := testutil.CreateTestOrg(t, db, "api-key-revoke-other", "API Key Revoke Other")
	key, token, err := apikeys.Create(t.Context(), db, organizationID, userID, keyOptions("Production"))
	require.NoError(t, err)

	authenticated, err := apikeys.Authenticate(t.Context(), db, token)
	require.NoError(t, err)
	require.Equal(t, key.ExternalID, authenticated.ExternalID)

	revoked, err := apikeys.RevokeByExternalID(t.Context(), db, otherOrganizationID, key.ExternalID)
	require.NoError(t, err)
	require.False(t, revoked)
	revoked, err = apikeys.RevokeByExternalID(t.Context(), db, organizationID, key.ExternalID)
	require.NoError(t, err)
	require.True(t, revoked)

	authenticated, err = apikeys.Authenticate(t.Context(), db, token)
	require.NoError(t, err)
	require.Nil(t, authenticated)
	keys, err := apikeys.List(t.Context(), db, organizationID)
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestAuthenticateRejectsMalformedAndUnknownTokens(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	for _, token := range []string{"", "secret", "nautilus_short", "nautilus_!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"} {
		key, err := apikeys.Authenticate(t.Context(), db, token)
		require.NoError(t, err)
		require.Nil(t, key)
	}
	key, err := apikeys.Authenticate(t.Context(), db, "nautilus_0123456789012345678901234567890123456789012")
	require.NoError(t, err)
	require.Nil(t, key)
}

func TestCreateValidatesFieldsAndScopes(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	organizationID, userID := apiKeyOwner(t, db, "validate")

	tests := map[string]*apikeys.CreateOptions{
		"missing options": nil,
		"missing name":    {Scopes: []apikeys.Scope{apikeys.ScopeRead}},
		"long name":       {Name: strings.Repeat("a", apikeys.MaxNameLength+1), Scopes: []apikeys.Scope{apikeys.ScopeRead}},
		"missing scopes":  {Name: "Production"},
		"invalid scope":   {Name: "Production", Scopes: []apikeys.Scope{"admin"}},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := apikeys.Create(t.Context(), db, organizationID, userID, options)
			require.Error(t, err)
		})
	}
}

func apiKeyOwner(t *testing.T, db database.Database, suffix string) (int, int) {
	t.Helper()
	userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "api-key-" + suffix})
	organizationID := testutil.CreateTestOrg(t, db, "api-key-"+suffix, "API Key "+suffix)
	return organizationID, userID
}

func keyOptions(name string) *apikeys.CreateOptions {
	return &apikeys.CreateOptions{Name: name, Scopes: []apikeys.Scope{apikeys.ScopeRead}}
}
