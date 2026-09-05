package users_test

import (
	"context"
	"testing"

	"nautilus/internal/crypto/argon2"
	"nautilus/internal/database"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRegister(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		email    optional.Optional[string]
	}{
		{
			name:     "with email",
			username: "testuser",
			email:    optional.Set("test@example.com"),
		},
		{
			name:     "without email",
			username: "noemail_user",
			email:    optional.Empty[string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := context.Background()

			user, err := users.Register(ctx, db, optional.Set(tt.username), tt.email, "password123")

			require.NoError(t, err)
			require.NotNil(t, user)
			require.NotZero(t, user.ID)
			require.NotEmpty(t, user.ExternalID)
			require.Equal(t, tt.username, user.Username.Data)
			require.Equal(t, enums.AuthProviderLocal, user.AuthProvider)
			require.False(t, user.Verified)
			require.False(t, user.Admin)

			if tt.email.Set {
				require.True(t, user.Email.Set)
				require.Equal(t, tt.email.Data, user.Email.Data)
				return
			}
			require.False(t, user.Email.Set)
		})
	}
}

func TestRegisterWithAuthProvider(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	user, err := users.RegisterWithAuthProvider(ctx, db,
		optional.Set("oauth_user"),
		optional.Set("oauth@example.com"),
		enums.AuthProviderGoogle,
		"google-auth-token-123",
	)

	require.NoError(t, err)
	require.NotNil(t, user)
	require.NotZero(t, user.ID)
	require.NotEmpty(t, user.ExternalID)
	require.Equal(t, "oauth_user", user.Username.Data)
	require.Equal(t, "oauth@example.com", user.Email.Data)
	require.Equal(t, enums.AuthProviderGoogle, user.AuthProvider)
	require.True(t, user.Verified)
	require.False(t, user.Admin)
}

func TestGetRoundTripsRegisteredUser(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created := registerUser(t, ctx, db, "roundtripuser", "roundtrip@example.com", "password123")

	user, err := users.Get(ctx, db, created.ID)
	require.NoError(t, err)
	requireUser(t, user, created.ID, created.ExternalID, "roundtripuser", "roundtrip@example.com")

	user, err = users.GetByEmail(ctx, db, "roundtrip@example.com")
	require.NoError(t, err)
	requireUser(t, user, created.ID, created.ExternalID, "roundtripuser", "roundtrip@example.com")

	user, err = users.GetByEmail(ctx, db, "ROUNDTRIP@EXAMPLE.COM")
	require.NoError(t, err)
	requireUser(t, user, created.ID, created.ExternalID, "roundtripuser", "roundtrip@example.com")

	external, err := users.GetExternal(ctx, db, created.ExternalID)
	require.NoError(t, err)
	requireExternalUser(t, external, created.ID, created.ExternalID, "roundtripuser")

	external, err = users.GetExternalUsername(ctx, db, "roundtripuser")
	require.NoError(t, err)
	requireExternalUser(t, external, created.ID, created.ExternalID, "roundtripuser")
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	user, err := users.Get(ctx, db, 999999)
	require.NoError(t, err)
	require.Nil(t, user)

	user, err = users.GetByEmail(ctx, db, "nonexistent@example.com")
	require.NoError(t, err)
	require.Nil(t, user)

	external, err := users.GetExternal(ctx, db, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	require.Nil(t, external)

	external, err = users.GetExternalUsername(ctx, db, "nonexistentuser")
	require.NoError(t, err)
	require.Nil(t, external)

	user, err = users.GetByAuthProvider(ctx, db, enums.AuthProviderGoogle, "nonexistent-token")
	require.NoError(t, err)
	require.Nil(t, user)
}

func TestEmailExists(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	registerUser(t, ctx, db, "existsuser", "exists@example.com", "password123")

	checks := []struct {
		email string
		want  bool
	}{
		{email: "exists@example.com", want: true},
		{email: "EXISTS@EXAMPLE.COM", want: true},
		{email: "notexists@example.com", want: false},
	}

	for _, tt := range checks {
		exists, err := users.EmailExists(ctx, db, tt.email)
		require.NoError(t, err)
		require.Equal(t, tt.want, exists)
	}
}

func TestUsernameExists(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	registerUser(t, ctx, db, "uniquename", "unique@example.com", "password123")

	checks := []struct {
		username string
		want     bool
	}{
		{username: "uniquename", want: true},
		{username: "UNIQUENAME", want: true},
		{username: "notunique", want: false},
	}

	for _, tt := range checks {
		exists, err := users.UsernameExists(ctx, db, tt.username)
		require.NoError(t, err)
		require.Equal(t, tt.want, exists)
	}
}

func TestGetPassword(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created := registerUser(t, ctx, db, "passuser", "pass@example.com", "mypassword123")

	id, hash, mfaEnabled, err := users.GetPassword(ctx, db, "pass@example.com")
	require.NoError(t, err)
	require.Equal(t, created.ID, id)
	require.NotEmpty(t, hash)
	require.False(t, mfaEnabled)

	match, err := argon2.Compare("mypassword123", hash)
	require.NoError(t, err)
	require.True(t, match)

	_, err = users.RegisterWithAuthProvider(ctx, db,
		optional.Set("oauthnopass"),
		optional.Set("oauthnopass@example.com"),
		enums.AuthProviderGitHub,
		"github-token",
	)
	require.NoError(t, err)

	for _, email := range []string{"nonexistent@example.com", "oauthnopass@example.com"} {
		id, hash, mfaEnabled, err = users.GetPassword(ctx, db, email)
		require.NoError(t, err)
		require.Equal(t, -1, id)
		require.Empty(t, hash)
		require.False(t, mfaEnabled)
	}
}

func TestGetAuthProvider(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	localUser := registerUser(t, ctx, db, "localauth", "local@example.com", "password123")

	id, provider, err := users.GetAuthProvider(ctx, db, "local@example.com")
	require.NoError(t, err)
	require.Equal(t, localUser.ID, id)
	require.Equal(t, enums.AuthProviderLocal, provider)

	oauthUser, err := users.RegisterWithAuthProvider(ctx, db,
		optional.Set("oauthauth"),
		optional.Set("oauth@example.com"),
		enums.AuthProviderMicrosoft,
		"ms-token",
	)
	require.NoError(t, err)

	id, provider, err = users.GetAuthProvider(ctx, db, "oauth@example.com")
	require.NoError(t, err)
	require.Equal(t, oauthUser.ID, id)
	require.Equal(t, enums.AuthProviderMicrosoft, provider)
}

func TestGetByAuthProvider(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created, err := users.RegisterWithAuthProvider(ctx, db,
		optional.Set("provideruser"),
		optional.Set("provider@example.com"),
		enums.AuthProviderApple,
		"apple-token-unique",
	)
	require.NoError(t, err)

	user, err := users.GetByAuthProvider(ctx, db, enums.AuthProviderApple, "apple-token-unique")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, created.ID, user.ID)
	require.Equal(t, "provideruser", user.Username.Data)

	user, err = users.GetByAuthProvider(ctx, db, enums.AuthProviderGoogle, "apple-token-unique")
	require.NoError(t, err)
	require.Nil(t, user)
}

func TestUpdatePassword(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created := registerUser(t, ctx, db, "updatepass", "updatepass@example.com", "oldpassword")

	err := users.UpdatePassword(ctx, db, created.ID, "newpassword123")
	require.NoError(t, err)

	_, hash, _, err := users.GetPassword(ctx, db, "updatepass@example.com")
	require.NoError(t, err)

	match, err := argon2.Compare("newpassword123", hash)
	require.NoError(t, err)
	require.True(t, match)

	match, err = argon2.Compare("oldpassword", hash)
	require.NoError(t, err)
	require.False(t, match)
}

func TestSetVerification(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created := registerUser(t, ctx, db, "verifyuser", "verify@example.com", "password123")
	require.False(t, created.Verified)

	err := users.SetVerification(ctx, db, created.ID, true)
	require.NoError(t, err)

	user, err := users.Get(ctx, db, created.ID)
	require.NoError(t, err)
	require.True(t, user.Verified)

	err = users.SetVerification(ctx, db, created.ID, false)
	require.NoError(t, err)

	user, err = users.Get(ctx, db, created.ID)
	require.NoError(t, err)
	require.False(t, user.Verified)
}

func TestUpdateEmail(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created := registerUser(t, ctx, db, "emailchange", "old@example.com", "password123")

	err := users.UpdateEmail(ctx, db, created.ID, "new@example.com")
	require.NoError(t, err)

	user, err := users.Get(ctx, db, created.ID)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", user.Email.Data)

	oldUser, err := users.GetByEmail(ctx, db, "old@example.com")
	require.NoError(t, err)
	require.Nil(t, oldUser)

	newUser, err := users.GetByEmail(ctx, db, "new@example.com")
	require.NoError(t, err)
	require.NotNil(t, newUser)
	require.Equal(t, created.ID, newUser.ID)
}

func TestUpdateUsername(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	created := registerUser(t, ctx, db, "oldusername", "username@example.com", "password123")

	err := users.UpdateUsername(ctx, db, created.ID, "newusername")
	require.NoError(t, err)

	user, err := users.Get(ctx, db, created.ID)
	require.NoError(t, err)
	require.Equal(t, "newusername", user.Username.Data)

	exists, err := users.UsernameExists(ctx, db, "oldusername")
	require.NoError(t, err)
	require.False(t, exists)

	exists, err = users.UsernameExists(ctx, db, "newusername")
	require.NoError(t, err)
	require.True(t, exists)
}

func registerUser(
	t *testing.T,
	ctx context.Context,
	db database.Database,
	username string,
	email string,
	password string,
) *users.User {
	t.Helper()

	user, err := users.Register(ctx, db, optional.Set(username), optional.Set(email), password)
	require.NoError(t, err)
	require.NotNil(t, user)
	return user
}

func requireUser(
	t *testing.T,
	user *users.User,
	id int,
	externalID string,
	username string,
	email string,
) {
	t.Helper()

	require.NotNil(t, user)
	require.Equal(t, id, user.ID)
	require.Equal(t, externalID, user.ExternalID)
	require.Equal(t, username, user.Username.Data)
	require.Equal(t, email, user.Email.Data)
}

func requireExternalUser(
	t *testing.T,
	user *users.UserExternal,
	id int,
	externalID string,
	username string,
) {
	t.Helper()

	require.NotNil(t, user)
	require.Equal(t, id, user.ID)
	require.Equal(t, externalID, user.ExternalID)
	require.Equal(t, username, user.Username)
}
