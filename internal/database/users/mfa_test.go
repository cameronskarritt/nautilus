package users_test

import (
	"context"
	"testing"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/users"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestPendingTOTP(t *testing.T) {
	t.Parallel()
	db, ctx, userID := setupMFATest(t)

	firstSecret := "FIRSTSECRET12345"
	expiresAt := time.Now().Add(10 * time.Minute)
	setPendingTOTP(t, ctx, db, userID, firstSecret, expiresAt)

	pending := requirePendingTOTP(t, ctx, db, userID)
	require.Equal(t, firstSecret, pending.Secret)
	require.True(t, pending.ExpiresAt.After(time.Now()))

	secondSecret := "SECONDSECRET6789"
	setPendingTOTP(t, ctx, db, userID, secondSecret, time.Now().Add(10*time.Minute))

	pending = requirePendingTOTP(t, ctx, db, userID)
	require.Equal(t, secondSecret, pending.Secret)
}

func TestGetPendingTOTPUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, ctx context.Context, db database.Database, userID int) int
	}{
		{
			name: "no pending secret exists",
			prepare: func(t *testing.T, ctx context.Context, db database.Database, userID int) int {
				t.Helper()
				return userID
			},
		},
		{
			name: "secret has expired",
			prepare: func(t *testing.T, ctx context.Context, db database.Database, userID int) int {
				t.Helper()
				setPendingTOTP(t, ctx, db, userID, "EXPIREDSECRET123", time.Now().Add(-time.Minute))
				return userID
			},
		},
		{
			name: "MFA is already enabled",
			prepare: func(t *testing.T, ctx context.Context, db database.Database, userID int) int {
				t.Helper()
				enableMFAWithSecret(t, ctx, db, userID, "MFAENABLEDSECRET")
				return userID
			},
		},
		{
			name: "user does not exist",
			prepare: func(t *testing.T, ctx context.Context, db database.Database, userID int) int {
				t.Helper()
				return 999999
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, ctx, userID := setupMFATest(t)
			userID = tt.prepare(t, ctx, db, userID)

			pending, err := users.GetPendingTOTP(ctx, db, userID)
			require.NoError(t, err)
			require.Nil(t, pending)
		})
	}
}

func TestSetPendingTOTPDoesNotUpdateEnabledUser(t *testing.T) {
	t.Parallel()
	db, ctx, userID := setupMFATest(t)

	originalSecret := "INITIALSECRET123"
	enableMFAWithSecret(t, ctx, db, userID, originalSecret)

	err := users.SetPendingTOTP(ctx, db, userID, "NEWSECRET1234567", time.Now().Add(10*time.Minute))
	require.NoError(t, err)

	enabled, err := users.HasMFAEnabled(ctx, db, userID)
	require.NoError(t, err)
	require.True(t, enabled)

	secret, err := users.GetTOTPSecret(ctx, db, userID)
	require.NoError(t, err)
	require.Equal(t, originalSecret, secret)

	pending, err := users.GetPendingTOTP(ctx, db, userID)
	require.NoError(t, err)
	require.Nil(t, pending)
}

func TestEnableMFA(t *testing.T) {
	t.Parallel()
	db, ctx, userID := setupMFATest(t)

	secret := "ENABLEMFASECRET1"
	setPendingTOTP(t, ctx, db, userID, secret, time.Now().Add(10*time.Minute))
	requirePendingTOTP(t, ctx, db, userID)

	err := users.EnableMFA(ctx, db, userID)
	require.NoError(t, err)

	enabled, err := users.HasMFAEnabled(ctx, db, userID)
	require.NoError(t, err)
	require.True(t, enabled)

	pending, err := users.GetPendingTOTP(ctx, db, userID)
	require.NoError(t, err)
	require.Nil(t, pending)

	retrievedSecret, err := users.GetTOTPSecret(ctx, db, userID)
	require.NoError(t, err)
	require.Equal(t, secret, retrievedSecret)
}

func TestDisableMFA(t *testing.T) {
	t.Parallel()
	db, ctx, userID := setupMFATest(t)

	firstSecret := "FIRSTMFASECRET12"
	enableMFAWithSecret(t, ctx, db, userID, firstSecret)

	enabled, err := users.HasMFAEnabled(ctx, db, userID)
	require.NoError(t, err)
	require.True(t, enabled)

	retrievedSecret, err := users.GetTOTPSecret(ctx, db, userID)
	require.NoError(t, err)
	require.Equal(t, firstSecret, retrievedSecret)

	err = users.DisableMFA(ctx, db, userID)
	require.NoError(t, err)

	enabled, err = users.HasMFAEnabled(ctx, db, userID)
	require.NoError(t, err)
	require.False(t, enabled)

	retrievedSecret, err = users.GetTOTPSecret(ctx, db, userID)
	require.NoError(t, err)
	require.Empty(t, retrievedSecret)

	pending, err := users.GetPendingTOTP(ctx, db, userID)
	require.NoError(t, err)
	require.Nil(t, pending)

	secondSecret := "SECONDMFASECRET1"
	enableMFAWithSecret(t, ctx, db, userID, secondSecret)

	retrievedSecret, err = users.GetTOTPSecret(ctx, db, userID)
	require.NoError(t, err)
	require.Equal(t, secondSecret, retrievedSecret)
}

func TestGetTOTPSecretUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, ctx context.Context, db database.Database, userID int) int
	}{
		{
			name: "MFA not enabled",
			prepare: func(t *testing.T, ctx context.Context, db database.Database, userID int) int {
				t.Helper()
				setPendingTOTP(t, ctx, db, userID, "PENDINGNOTENABLED", time.Now().Add(10*time.Minute))
				return userID
			},
		},
		{
			name: "user does not exist",
			prepare: func(t *testing.T, ctx context.Context, db database.Database, userID int) int {
				t.Helper()
				return 999999
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, ctx, userID := setupMFATest(t)
			userID = tt.prepare(t, ctx, db, userID)

			secret, err := users.GetTOTPSecret(ctx, db, userID)
			require.NoError(t, err)
			require.Empty(t, secret)
		})
	}
}

func TestHasMFAEnabled(t *testing.T) {
	t.Parallel()
	db, ctx, userID := setupMFATest(t)

	enabled, err := users.HasMFAEnabled(ctx, db, userID)
	require.NoError(t, err)
	require.False(t, enabled)

	enableMFAWithSecret(t, ctx, db, userID, "HASMFAENABLED123")

	enabled, err = users.HasMFAEnabled(ctx, db, userID)
	require.NoError(t, err)
	require.True(t, enabled)

	enabled, err = users.HasMFAEnabled(ctx, db, 999999)
	require.NoError(t, err)
	require.False(t, enabled)
}

func setupMFATest(t *testing.T) (database.Database, context.Context, int) {
	t.Helper()

	db := testutil.SetupTestDB(t)
	ctx := testutil.ContextWithEncrypter(t)
	userID := testutil.CreateTestUser(t, db, nil)
	return db, ctx, userID
}

func setPendingTOTP(
	t *testing.T,
	ctx context.Context,
	db database.Database,
	userID int,
	secret string,
	expiresAt time.Time,
) {
	t.Helper()

	err := users.SetPendingTOTP(ctx, db, userID, secret, expiresAt)
	require.NoError(t, err)
}

func enableMFAWithSecret(
	t *testing.T,
	ctx context.Context,
	db database.Database,
	userID int,
	secret string,
) {
	t.Helper()

	setPendingTOTP(t, ctx, db, userID, secret, time.Now().Add(10*time.Minute))

	err := users.EnableMFA(ctx, db, userID)
	require.NoError(t, err)
}

func requirePendingTOTP(
	t *testing.T,
	ctx context.Context,
	db database.Database,
	userID int,
) *users.PendingTOTP {
	t.Helper()

	pending, err := users.GetPendingTOTP(ctx, db, userID)
	require.NoError(t, err)
	require.NotNil(t, pending)
	return pending
}
