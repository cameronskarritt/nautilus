package authcodes_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"nautilus/internal/database/authcodes"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCreate_WithData(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)
	data := &authcodes.ChangeEmailData{OldEmail: "old@example.com", NewEmail: "new@example.com"}

	token, err := authcodes.Create(ctx, db, enums.AuthCodeEmailChange, userID, time.Hour, data)

	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Len(t, token, 32)

	code, err := authcodes.Verify(ctx, db, enums.AuthCodeEmailChange, token)
	require.NoError(t, err)
	require.NotNil(t, code)
	var decoded authcodes.ChangeEmailData
	err = code.UnmarshalData(&decoded)
	require.NoError(t, err)
	require.Equal(t, "old@example.com", decoded.OldEmail)
	require.Equal(t, "new@example.com", decoded.NewEmail)
}

func TestCreate_WithoutData(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	token, err := authcodes.Create(ctx, db, enums.AuthCodeRecovery, userID, time.Hour, nil)

	require.NoError(t, err)
	require.NotEmpty(t, token)
	code, err := authcodes.Verify(ctx, db, enums.AuthCodeRecovery, token)
	require.NoError(t, err)
	require.NotNil(t, code)
	require.Nil(t, code.Data)
}

func TestVerify_Success(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	token, err := authcodes.Create(ctx, db, enums.AuthCodeVerification, userID, time.Hour, nil)
	require.NoError(t, err)
	require.Len(t, token, 32)

	code, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, token)
	require.NoError(t, err)
	require.NotNil(t, code)
	require.Equal(t, userID, code.UserID)
}

func TestVerify_InvalidHexToken(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	code, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, "not-valid-hex!!!")
	require.NoError(t, err)
	require.Nil(t, code)
}

func TestVerify_NonExistentToken(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	// Valid 32-char hex that does not exist in DB
	nonExistent := hex.EncodeToString(make([]byte, 16))

	code, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, nonExistent)
	require.Error(t, err)
	require.Nil(t, code)
}

func TestVerify_ExpiredToken(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	token, err := authcodes.Create(ctx, db, enums.AuthCodeVerification, userID, 1*time.Millisecond, nil)
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)

	code, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, token)
	require.NoError(t, err)
	require.Nil(t, code)
}

func TestVerify_OneTimeUse(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	token, err := authcodes.Create(ctx, db, enums.AuthCodeVerification, userID, time.Hour, nil)
	require.NoError(t, err)

	code1, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, token)
	require.NoError(t, err)
	require.NotNil(t, code1)

	// Second verify returns error (no rows, token already consumed)
	code2, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, token)
	require.Error(t, err)
	require.Nil(t, code2)
}

func TestCreate_DeactivatesPreviousCodes(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	token1, err := authcodes.Create(ctx, db, enums.AuthCodeVerification, userID, time.Hour, nil)
	require.NoError(t, err)
	token2, err := authcodes.Create(ctx, db, enums.AuthCodeVerification, userID, time.Hour, nil)
	require.NoError(t, err)

	// First token should be invalidated (soft-deleted), verify returns error
	code1, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, token1)
	require.Error(t, err)
	require.Nil(t, code1)

	// Second token should work
	code2, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, token2)
	require.NoError(t, err)
	require.NotNil(t, code2)
	require.Equal(t, userID, code2.UserID)
}

func TestCreate_DifferentCodeTypesDoNotConflict(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	userID := testutil.CreateTestUser(t, db, nil)

	verificationToken, err := authcodes.Create(ctx, db, enums.AuthCodeVerification, userID, time.Hour, nil)
	require.NoError(t, err)
	recoveryToken, err := authcodes.Create(ctx, db, enums.AuthCodeRecovery, userID, time.Hour, nil)
	require.NoError(t, err)

	verificationCode, err := authcodes.Verify(ctx, db, enums.AuthCodeVerification, verificationToken)
	require.NoError(t, err)
	require.NotNil(t, verificationCode)
	require.Equal(t, userID, verificationCode.UserID)

	recoveryCode, err := authcodes.Verify(ctx, db, enums.AuthCodeRecovery, recoveryToken)
	require.NoError(t, err)
	require.NotNil(t, recoveryCode)
	require.Equal(t, userID, recoveryCode.UserID)
}

func TestAuthCode_UnmarshalData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Data          []byte
		Target        any
		ExpectedError bool
	}{
		{
			Name:          "valid JSON unmarshals into struct",
			Data:          []byte(`{"old_email":"a@x.com","new_email":"b@x.com"}`),
			Target:        &authcodes.ChangeEmailData{},
			ExpectedError: false,
		},
		{
			Name:          "invalid JSON returns error",
			Data:          []byte(`{invalid`),
			Target:        &authcodes.ChangeEmailData{},
			ExpectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			code := authcodes.AuthCode{UserID: 1, Data: tt.Data}
			err := code.UnmarshalData(tt.Target)

			if !tt.ExpectedError {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}
