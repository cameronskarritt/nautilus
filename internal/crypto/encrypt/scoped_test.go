package encrypt

import (
	"bytes"
	"context"
	"testing"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestScopedEncrypters(t *testing.T) {
	t.Parallel()
	keys := &keyManager{}
	user := ForUser(keys)
	first := ForOrganization(keys, "first")
	second := ForOrganization(keys, "second")
	require.Zero(t, keys.calls)
	for _, enc := range []*Encrypter{user, first, second} {
		ciphertext, err := enc.Encrypt(t.Context(), []byte("secret"))
		require.NoError(t, err)
		require.Equal(t, make([]byte, 32), keys.returned)
		plaintext, err := enc.Decrypt(t.Context(), ciphertext)
		require.NoError(t, err)
		require.Equal(t, []byte("secret"), plaintext)
		for _, other := range []*Encrypter{user, first, second} {
			if enc != other {
				plaintext, err := other.Decrypt(t.Context(), ciphertext)
				require.Error(t, err)
				require.Nil(t, plaintext)
			}
		}
	}
	require.True(t, keys.bounded)
}

func TestScopedEncrypterPreservesLegacyCiphertext(t *testing.T) {
	t.Parallel()
	raw, err := New(bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	ciphertext, err := raw.Encrypt(t.Context(), []byte("legacy TOTP secret"))
	require.NoError(t, err)
	plaintext, err := ForUser(&keyManager{}).Decrypt(t.Context(), ciphertext)
	require.NoError(t, err)
	require.Equal(t, "legacy TOTP secret", string(plaintext))
}

func TestScopedEncrypterFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		length int
		err    error
	}{
		{name: "provider error", err: errors.New("key unavailable"), length: 32},
		{name: "empty key"},
		{name: "wrong length", length: 16},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			keys := &keyManager{override: true, length: tt.length, err: tt.err}
			data, err := ForOrganization(keys, "first").Encrypt(t.Context(), []byte("secret"))
			require.Error(t, err)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
			require.Nil(t, data)
			require.Equal(t, "first", keys.orgID)
			require.Equal(t, make([]byte, tt.length), keys.returned)
		})
	}
	for _, enc := range []*Encrypter{new(Encrypter), ForUser(nil), ForOrganization(nil, "first"), ForOrganization(&keyManager{}, "")} {
		_, err := enc.Encrypt(t.Context(), []byte("secret"))
		require.Error(t, err)
	}
}

func TestScopedEncrypterSkipsLookupWhenWorkCannotProceed(t *testing.T) {
	t.Parallel()
	keys := &keyManager{}
	enc := ForUser(keys)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := enc.Encrypt(ctx, []byte("secret"))
	require.ErrorIs(t, err, context.Canceled)
	_, err = enc.Decrypt(t.Context(), []byte("short"))
	require.Error(t, err)
	require.Zero(t, keys.calls)
}

func TestScopedEncrypterPassesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	enc := &Encrypter{key: func(ctx context.Context) ([]byte, error) {
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	_, err := enc.Encrypt(ctx, []byte("secret"))
	require.ErrorIs(t, err, context.Canceled)
}

type keyManager struct {
	calls    int
	orgID    string
	returned []byte
	bounded  bool
	override bool
	length   int
	err      error
}

func (m *keyManager) UserKey(ctx context.Context) ([]byte, error) {
	return m.resolve(ctx, "", 1)
}

func (m *keyManager) OrganizationKey(ctx context.Context, orgID string) ([]byte, error) {
	value := byte(2)
	if orgID == "second" {
		value = 3
	}
	return m.resolve(ctx, orgID, value)
}

func (m *keyManager) resolve(ctx context.Context, orgID string, value byte) ([]byte, error) {
	m.calls++
	m.orgID = orgID
	deadline, ok := ctx.Deadline()
	m.bounded = ok && time.Until(deadline) <= 10*time.Second
	length := 32
	if m.override {
		length = m.length
	}
	m.returned = bytes.Repeat([]byte{value}, length)
	return m.returned, m.err
}
