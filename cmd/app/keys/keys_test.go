package keys

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/kmskeys"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want *options
	}{
		{name: "user", args: []string{"provision-user", "--key-arn", "arn"}, want: &options{command: "provision-user", keyARN: "arn"}},
		{name: "import", args: []string{"import-user", "--key-arn", "arn"}, want: &options{command: "import-user", keyARN: "arn"}},
		{name: "organization", args: []string{"provision-organization", "--key-arn", "arn", "--org-id", "org"}, want: &options{command: "provision-organization", keyARN: "arn", orgID: "org"}},
		{name: "no command"},
		{name: "unknown command", args: []string{"rotate"}},
		{name: "missing key", args: []string{"provision-user"}},
		{name: "missing organization", args: []string{"provision-organization", "--key-arn", "arn"}},
		{name: "unexpected argument", args: []string{"provision-user", "--key-arn", "arn", "extra"}},
		{name: "user organization flag", args: []string{"import-user", "--key-arn", "arn", "--org-id", "org"}},
		{name: "plaintext key argument", args: []string{"import-user", "--key-arn", "arn", "--key", "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parse(tt.args)
			if tt.want == nil {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLegacyKey(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{7}, 32)
	for _, encoded := range []string{hex.EncodeToString(key), base64.StdEncoding.EncodeToString(key)} {
		got, err := legacyKey(encoded)
		require.NoError(t, err)
		require.Equal(t, key, got)
	}
	for _, encoded := range []string{"", "not-a-key", hex.EncodeToString(key[:16]), base64.StdEncoding.EncodeToString(key[:16])} {
		got, err := legacyKey(encoded)
		require.Error(t, err)
		require.Nil(t, got)
		if encoded != "" {
			require.NotContains(t, err.Error(), encoded)
		}
	}
}

func TestUserProvisioningPreservesExistingSecrets(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	require.NoError(t, allowNewUserKey(ctx, db))
	key := bytes.Repeat([]byte{3}, 32)
	enc, err := encrypt.New(key)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt([]byte("sensitive-fixture-42"))
	require.NoError(t, err)
	userID := testutil.CreateTestUser(t, db, nil)
	_, err = db.Exec(ctx, "UPDATE users SET totp_secret = $1, deleted_at = CURRENT_TIMESTAMP WHERE id = $2", ciphertext, userID)
	require.NoError(t, err)
	require.Error(t, allowNewUserKey(ctx, db))
	require.NoError(t, verifyLegacySecrets(ctx, db, key))
	wrongKey := bytes.Repeat([]byte{4}, 32)
	err = verifyLegacySecrets(ctx, db, wrongKey)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sensitive-fixture-42")
	_, err = kmskeys.CreateUser(ctx, db, "user-key", []byte("wrapped"))
	require.NoError(t, err)
	require.NoError(t, allowNewUserKey(ctx, db))
	var stored []byte
	require.NoError(t, db.QueryRow(ctx, "SELECT totp_secret FROM users WHERE id = $1", userID).Scan(&stored))
	require.Equal(t, ciphertext, stored)
}
