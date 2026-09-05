package signature

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestSign(t *testing.T) {
	t.Parallel()

	sig := Sign([]byte("secret"), []byte("data"))

	require.Equal(t, "GywWt1vSqHDBFBU8zaW8_KYzFLxyL6Fg1pDeEzzLuds", sig)
	require.True(t, Verify([]byte("secret"), []byte("data"), sig))
}

func TestVerify(t *testing.T) {
	t.Parallel()

	sig := Sign([]byte("secret"), []byte("data"))
	tests := []struct {
		Name      string
		Secret    []byte
		Data      []byte
		Signature string
		Expected  bool
	}{
		{
			Name:      "wrong secret",
			Secret:    []byte("wrong-secret"),
			Data:      []byte("data"),
			Signature: sig,
			Expected:  false,
		},
		{
			Name:      "wrong data",
			Secret:    []byte("secret"),
			Data:      []byte("different-data"),
			Signature: sig,
			Expected:  false,
		},
		{
			Name:      "invalid base64",
			Secret:    []byte("secret"),
			Data:      []byte("data"),
			Signature: "not-valid-base64!!!",
			Expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, Verify(tt.Secret, tt.Data, tt.Signature))
		})
	}
}

func TestStringWrappers(t *testing.T) {
	t.Parallel()

	sig := SignString("secret", "data")

	require.Equal(t, Sign([]byte("secret"), []byte("data")), sig)
	require.True(t, VerifyString("secret", "data", sig))
	require.False(t, VerifyString("wrong-secret", "data", sig))
	require.False(t, VerifyString("secret", "wrong-data", sig))
}
