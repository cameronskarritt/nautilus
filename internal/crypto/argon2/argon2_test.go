package argon2

import (
	"testing"

	"nautilus/internal/testutil/require"
)

const testHash = "$argon2id$v=19$m=1024,t=1,p=1$Kx3w6Znm4wwXSMKHmmWiRQ$iA7GCqaX/rva4BxXkRwRzA"

func TestGenerateHash(t *testing.T) {
	t.Parallel()

	params := parameters{
		memory:  1024,
		time:    1,
		threads: 1,
		saltLen: 8,
		keyLen:  16,
	}

	hash, err := generateHash("password", params)
	require.NoError(t, err)
	require.Contains(t, hash, "$argon2id$v=19$m=1024,t=1,p=1$")

	ok, err := Compare("password", hash)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = Compare("wrong-password", hash)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Plaintext     string
		Hash          string
		Expected      bool
		ExpectedError string
	}{
		{
			Name:      "password matches",
			Plaintext: "password",
			Hash:      testHash,
			Expected:  true,
		},
		{
			Name:      "password does not match",
			Plaintext: "drowssap",
			Hash:      testHash,
		},
		{
			Name:          "malformed hash",
			Plaintext:     "password",
			Hash:          "$argon2id$m=1024,t=1,p=1$Kx3w6Znm4wwX",
			ExpectedError: "malformed hash string",
		},
		{
			Name:          "unsupported variant",
			Plaintext:     "password",
			Hash:          "$argon2i$v=19$m=1024,t=1,p=1$Kx3w6Znm4wwXSMKHmmWiRQ$iA7GCqaX/rva4BxXkRwRzA",
			ExpectedError: "unsupported argon2 variant",
		},
		{
			Name:          "malformed version",
			Plaintext:     "password",
			Hash:          "$argon2id$v=x$m=1024,t=1,p=1$Kx3w6Znm4wwXSMKHmmWiRQ$iA7GCqaX/rva4BxXkRwRzA",
			ExpectedError: "unable to scan version",
		},
		{
			Name:          "version mismatch",
			Plaintext:     "password",
			Hash:          "$argon2id$v=18$m=1024,t=1,p=1$Kx3w6Znm4wwXSMKHmmWiRQ$iA7GCqaX/rva4BxXkRwRzA",
			ExpectedError: "argon2 version mismatch",
		},
		{
			Name:          "malformed params",
			Plaintext:     "password",
			Hash:          "$argon2id$v=19$m=1024,p=1$Kx3w6Znm4wwXSMKHmmWiRQ$iA7GCqaX/rva4BxXkRwRzA",
			ExpectedError: "unable to scan parameters",
		},
		{
			Name:          "invalid salt",
			Plaintext:     "password",
			Hash:          "$argon2id$v=19$m=1024,t=1,p=1$not-valid!$iA7GCqaX/rva4BxXkRwRzA",
			ExpectedError: "unable to decode salt",
		},
		{
			Name:          "invalid hash",
			Plaintext:     "password",
			Hash:          "$argon2id$v=19$m=1024,t=1,p=1$Kx3w6Znm4wwXSMKHmmWiRQ$not-valid!",
			ExpectedError: "unable to decode hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			ok, err := Compare(tt.Plaintext, tt.Hash)
			if tt.ExpectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.ExpectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, ok)
		})
	}
}
