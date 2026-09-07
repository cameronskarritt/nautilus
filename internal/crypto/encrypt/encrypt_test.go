package encrypt

import (
	"bytes"
	"testing"

	"nautilus/internal/testutil/require"
)

const (
	testHexKey    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testBase64Key = "ASNFZ4mrze8BI0VniavN7wEjRWeJq83vASNFZ4mrze8="
)

func TestNewEncrypter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Key           string
		ExpectedError bool
	}{
		{Name: "hex key", Key: testHexKey},
		{Name: "base64 key", Key: testBase64Key},
		{Name: "invalid key", Key: "not-valid", ExpectedError: true},
		{Name: "wrong key length", Key: "0123456789abcdef", ExpectedError: true},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			enc, err := NewEncrypter(tt.Key)
			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, enc)
		})
	}
}

func TestNewRejectsInvalidKeyLengths(t *testing.T) {
	t.Parallel()
	for _, length := range []int{0, 16, 24, 31, 33} {
		_, err := New(make([]byte, length))
		require.Error(t, err)
	}
}

func TestNewDoesNotRetainCallerKey(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{1}, 32)
	enc, err := New(key)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt(t.Context(), []byte("secret"))
	require.NoError(t, err)
	clear(key)
	plaintext, err := enc.Decrypt(t.Context(), ciphertext)
	require.NoError(t, err)
	require.Equal(t, "secret", string(plaintext))
}

func TestEncryptDecrypt(t *testing.T) {
	t.Parallel()

	enc := testEncrypter(t)
	tests := []struct {
		Name      string
		Plaintext []byte
	}{
		{Name: "empty", Plaintext: []byte{}},
		{Name: "text", Plaintext: []byte("hello")},
		{Name: "binary", Plaintext: []byte{0x00, 0x01, 0x02, 0xff}},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			ciphertext, err := enc.Encrypt(t.Context(), tt.Plaintext)
			require.NoError(t, err)
			require.Greater(t, len(ciphertext), len(tt.Plaintext))

			plaintext, err := enc.Decrypt(t.Context(), ciphertext)
			require.NoError(t, err)
			require.True(t, bytes.Equal(tt.Plaintext, plaintext))
		})
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	t.Parallel()

	enc := testEncrypter(t)
	plaintext := []byte("same plaintext")

	first, err := enc.Encrypt(t.Context(), plaintext)
	require.NoError(t, err)
	second, err := enc.Encrypt(t.Context(), plaintext)
	require.NoError(t, err)

	require.False(t, bytes.Equal(first, second))
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	t.Parallel()

	enc := testEncrypter(t)
	tests := []struct {
		Name       string
		Ciphertext []byte
	}{
		{Name: "too short", Ciphertext: []byte{0x01, 0x02}},
		{Name: "invalid ciphertext", Ciphertext: []byte("this is not valid ciphertext at all!!")},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			_, err := enc.Decrypt(t.Context(), tt.Ciphertext)
			require.Error(t, err)
		})
	}
}

func TestContextHelpers(t *testing.T) {
	t.Parallel()

	enc := testEncrypter(t)
	ctx := WithContext(t.Context(), enc)

	require.Equal(t, enc, FromContext(ctx))
	require.Nil(t, FromContext(t.Context()))
}

func testEncrypter(t *testing.T) *Encrypter {
	t.Helper()

	enc, err := NewEncrypter(testHexKey)
	require.NoError(t, err)
	return enc
}
