package recoverycodes_test

import (
	"context"
	"encoding/base32"
	"strings"
	"testing"

	"nautilus/internal/database/recoverycodes"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	t.Run("generates exactly 10 codes", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)
		require.Len(t, codes, 10)
	})

	t.Run("codes are valid base32 strings", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		b32 := base32.StdEncoding.WithPadding(base32.NoPadding)
		for _, code := range codes {
			_, err := b32.DecodeString(code)
			require.NoError(t, err)
		}
	})

	t.Run("each code is unique", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		seen := make(map[string]bool)
		for _, code := range codes {
			require.False(t, seen[code], "duplicate code found: %s", code)
			seen[code] = true
		}
	})

	t.Run("deletes existing codes before inserting new ones", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		// Generate first set
		firstCodes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)
		require.Len(t, firstCodes, 10)

		// Generate second set
		secondCodes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)
		require.Len(t, secondCodes, 10)

		// First codes should no longer work
		for _, code := range firstCodes {
			valid, err := recoverycodes.Verify(ctx, db, userID, code)
			require.NoError(t, err)
			require.False(t, valid, "old code should not be valid after regeneration")
		}

		// Second codes should work
		valid, err := recoverycodes.Verify(ctx, db, userID, secondCodes[0])
		require.NoError(t, err)
		require.True(t, valid)
	})
}

func TestVerify(t *testing.T) {
	t.Parallel()

	t.Run("returns true for valid code and marks as used", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Use the first code
		valid, err := recoverycodes.Verify(ctx, db, userID, codes[0])
		require.NoError(t, err)
		require.True(t, valid)

		// Count should decrease by 1
		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 9, count)
	})

	t.Run("returns false for already-used code", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Use the code
		valid, err := recoverycodes.Verify(ctx, db, userID, codes[0])
		require.NoError(t, err)
		require.True(t, valid)

		// Try to use it again
		valid, err = recoverycodes.Verify(ctx, db, userID, codes[0])
		require.NoError(t, err)
		require.False(t, valid)
	})

	t.Run("returns false for invalid code", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		_, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		valid, err := recoverycodes.Verify(ctx, db, userID, "INVALIDCODE")
		require.NoError(t, err)
		require.False(t, valid)
	})

	t.Run("case-insensitive verification", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Verify with lowercase
		lowercaseCode := strings.ToLower(codes[0])
		valid, err := recoverycodes.Verify(ctx, db, userID, lowercaseCode)
		require.NoError(t, err)
		require.True(t, valid)
	})

	t.Run("handles spaces in code input", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Add spaces to the code
		codeWithSpaces := codes[0][:4] + " " + codes[0][4:]
		valid, err := recoverycodes.Verify(ctx, db, userID, codeWithSpaces)
		require.NoError(t, err)
		require.True(t, valid)
	})

	t.Run("returns false for non-existent user", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		valid, err := recoverycodes.Verify(ctx, db, 999999, "SOMECODE")
		require.NoError(t, err)
		require.False(t, valid)
	})

	t.Run("codes from different users are isolated", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		user1ID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "user1"})
		user2ID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "user2"})

		codes1, err := recoverycodes.Generate(ctx, db, user1ID)
		require.NoError(t, err)

		_, err = recoverycodes.Generate(ctx, db, user2ID)
		require.NoError(t, err)

		// User1's code should not work for user2
		valid, err := recoverycodes.Verify(ctx, db, user2ID, codes1[0])
		require.NoError(t, err)
		require.False(t, valid)
	})
}

func TestDeleteAll(t *testing.T) {
	t.Parallel()

	t.Run("soft-deletes all codes for user", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Delete all codes
		err = recoverycodes.DeleteAll(ctx, db, userID)
		require.NoError(t, err)

		// Count should be 0
		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 0, count)

		// Codes should no longer verify
		for _, code := range codes {
			valid, err := recoverycodes.Verify(ctx, db, userID, code)
			require.NoError(t, err)
			require.False(t, valid)
		}
	})

	t.Run("does not affect other users codes", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		user1ID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "user1"})
		user2ID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "user2"})

		_, err := recoverycodes.Generate(ctx, db, user1ID)
		require.NoError(t, err)

		codes2, err := recoverycodes.Generate(ctx, db, user2ID)
		require.NoError(t, err)

		// Delete user1's codes
		err = recoverycodes.DeleteAll(ctx, db, user1ID)
		require.NoError(t, err)

		// User2's codes should still work
		valid, err := recoverycodes.Verify(ctx, db, user2ID, codes2[0])
		require.NoError(t, err)
		require.True(t, valid)
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		_, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Delete twice
		err = recoverycodes.DeleteAll(ctx, db, userID)
		require.NoError(t, err)

		err = recoverycodes.DeleteAll(ctx, db, userID)
		require.NoError(t, err)

		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestCountRemaining(t *testing.T) {
	t.Parallel()

	t.Run("returns 10 after fresh generation", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		_, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 10, count)
	})

	t.Run("decrements after each code use", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Use 3 codes
		for i := 0; i < 3; i++ {
			valid, err := recoverycodes.Verify(ctx, db, userID, codes[i])
			require.NoError(t, err)
			require.True(t, valid)
		}

		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 7, count)
	})

	t.Run("returns 0 after all codes used", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		codes, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		// Use all codes
		for _, code := range codes {
			valid, err := recoverycodes.Verify(ctx, db, userID, code)
			require.NoError(t, err)
			require.True(t, valid)
		}

		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("returns 0 after all codes deleted", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		_, err := recoverycodes.Generate(ctx, db, userID)
		require.NoError(t, err)

		err = recoverycodes.DeleteAll(ctx, db, userID)
		require.NoError(t, err)

		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("returns 0 for user with no codes", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()
		userID := testutil.CreateTestUser(t, db, nil)

		// No codes generated
		count, err := recoverycodes.CountRemaining(ctx, db, userID)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("returns 0 for non-existent user", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		count, err := recoverycodes.CountRemaining(ctx, db, 999999)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}
