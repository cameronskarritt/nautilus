package encrypt

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		plaintext []byte
	}{
		{name: "empty", plaintext: []byte{}},
		{name: "secret", plaintext: []byte("TOTP secret")},
		{name: "maximum", plaintext: bytes.Repeat([]byte{42}, MaxPlaintextSize)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			keys := &keyManager{}
			enc := ForUser(keys)
			binding := Binding{Purpose: "totp", RecordID: "user-id"}
			frame, err := enc.Seal(t.Context(), tt.plaintext, binding)
			require.NoError(t, err)
			require.Equal(t, make([]byte, 32), keys.returned)
			plaintext, err := enc.Open(t.Context(), frame, binding)
			require.NoError(t, err)
			require.True(t, bytes.Equal(tt.plaintext, plaintext))
			require.Equal(t, make([]byte, 32), keys.returned)
			require.True(t, keys.bounded)
		})
	}
}

func TestEnvelopeRandomization(t *testing.T) {
	t.Parallel()
	enc := ForUser(&keyManager{})
	binding := Binding{Purpose: "totp", RecordID: "user-id"}
	first, err := enc.Seal(t.Context(), []byte("secret"), binding)
	require.NoError(t, err)
	second, err := enc.Seal(t.Context(), []byte("secret"), binding)
	require.NoError(t, err)
	require.NotEqual(t, first[9:21], second[9:21])
	require.NotEqual(t, first[21:33], second[21:33])
	require.NotEqual(t, first[9:21], first[21:33])

	// Unwrapping confirms that every write generates a new 256-bit data key.
	wrap, err := newCipher(bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	firstKey, err := wrap.Open(nil, first[9:21], first[33:81], enc.associatedData(first[:33], binding, "wrap"))
	defer clear(firstKey)
	require.NoError(t, err)
	secondKey, err := wrap.Open(nil, second[9:21], second[33:81], enc.associatedData(second[:33], binding, "wrap"))
	defer clear(secondKey)
	require.NoError(t, err)
	require.Len(t, firstKey, 32)
	require.Len(t, secondKey, 32)
	require.NotEqual(t, firstKey, secondKey)
}

func TestEnvelopeBinding(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		scope   string
		binding Binding
	}{
		{name: "other organization", scope: "second", binding: Binding{Purpose: "ab", RecordID: "c"}},
		{name: "users", binding: Binding{Purpose: "ab", RecordID: "c"}},
		{name: "purpose", scope: "first", binding: Binding{Purpose: "other", RecordID: "c"}},
		{name: "record", scope: "first", binding: Binding{Purpose: "ab", RecordID: "other"}},
		{name: "ambiguous concatenation", scope: "first", binding: Binding{Purpose: "a", RecordID: "bc"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			keys := &sameEnvelopeKeys{}
			frame, err := ForOrganization(keys, "first").Seal(t.Context(), []byte("secret"), Binding{Purpose: "ab", RecordID: "c"})
			require.NoError(t, err)
			other := ForUser(keys)
			if tt.scope != "" {
				other = ForOrganization(keys, tt.scope)
			}
			plaintext, err := other.Open(t.Context(), frame, tt.binding)
			require.ErrorIs(t, err, ErrInvalidEnvelope)
			require.Nil(t, plaintext)
		})
	}
}

func TestEnvelopeTampering(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		offset int
	}{
		{name: "magic", offset: 0},
		{name: "version", offset: 4},
		{name: "length", offset: 8},
		{name: "wrap nonce", offset: 9},
		{name: "data nonce", offset: 21},
		{name: "wrapped key", offset: 33},
		{name: "wrapped key tag", offset: 65},
		{name: "ciphertext", offset: 81},
		{name: "data tag", offset: 87},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enc := ForUser(&keyManager{})
			binding := Binding{Purpose: "totp", RecordID: "user-id"}
			frame, err := enc.Seal(t.Context(), []byte("secret"), binding)
			require.NoError(t, err)
			frame[tt.offset] ^= 1
			plaintext, err := enc.Open(t.Context(), frame, binding)
			require.ErrorIs(t, err, ErrInvalidEnvelope)
			require.Nil(t, plaintext)
		})
	}
}

func TestEnvelopeRejectsMalformedBeforeLookup(t *testing.T) {
	t.Parallel()
	binding := Binding{Purpose: "totp", RecordID: "user-id"}
	valid, err := ForUser(&keyManager{}).Seal(t.Context(), []byte("secret"), binding)
	require.NoError(t, err)
	for _, tt := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{name: "empty", change: func([]byte) []byte { return nil }},
		{name: "short header", change: func(b []byte) []byte { return b[:12] }},
		{name: "short frame", change: func(b []byte) []byte { return b[:96] }},
		{name: "truncated", change: func(b []byte) []byte { return b[:len(b)-1] }},
		{name: "trailing bytes", change: func(b []byte) []byte { return append(b, 0) }},
		{name: "magic", change: func(b []byte) []byte { b[0] = 0; return b }},
		{name: "version", change: func(b []byte) []byte { b[4] = 2; return b }},
		{name: "oversized declaration", change: func(b []byte) []byte { binary.BigEndian.PutUint32(b[5:9], ^uint32(0)); return b }},
		{name: "oversized frame", change: func(b []byte) []byte { return append(b, make([]byte, MaxPlaintextSize)...) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			keys := &keyManager{}
			plaintext, err := ForUser(keys).Open(t.Context(), tt.change(bytes.Clone(valid)), binding)
			require.ErrorIs(t, err, ErrInvalidEnvelope)
			require.Nil(t, plaintext)
			require.Zero(t, keys.calls)
		})
	}
}

func TestEnvelopeRejectsInvalidBindingBeforeLookup(t *testing.T) {
	t.Parallel()
	for _, binding := range []Binding{
		{},
		{Purpose: "totp"},
		{RecordID: "id"},
		{Purpose: strings.Repeat("p", maxBindingSize+1), RecordID: "id"},
		{Purpose: "totp", RecordID: strings.Repeat("i", maxBindingSize+1)},
	} {
		keys := &keyManager{}
		enc := ForUser(keys)
		_, err := enc.Seal(t.Context(), nil, binding)
		require.ErrorIs(t, err, ErrInvalidBinding)
		_, err = enc.Open(t.Context(), nil, binding)
		require.ErrorIs(t, err, ErrInvalidBinding)
		require.Zero(t, keys.calls)
	}
	enc := ForUser(&keyManager{})
	binding := Binding{Purpose: strings.Repeat("p", maxBindingSize), RecordID: strings.Repeat("i", maxBindingSize)}
	frame, err := enc.Seal(t.Context(), nil, binding)
	require.NoError(t, err)
	_, err = enc.Open(t.Context(), frame, binding)
	require.NoError(t, err)
}

func TestEnvelopeRequiresScope(t *testing.T) {
	t.Parallel()
	for _, enc := range []*Encrypter{nil, {}, ForUser(nil), ForOrganization(nil, "id"), ForOrganization(&keyManager{}, "")} {
		_, err := enc.Seal(t.Context(), nil, Binding{Purpose: "totp", RecordID: "id"})
		require.ErrorIs(t, err, ErrUnscoped)
		_, err = enc.Open(t.Context(), nil, Binding{Purpose: "totp", RecordID: "id"})
		require.ErrorIs(t, err, ErrUnscoped)
	}
}

func TestEnvelopeRejectsLargePlaintextBeforeLookup(t *testing.T) {
	t.Parallel()
	keys := &keyManager{}
	_, err := ForUser(keys).Seal(t.Context(), make([]byte, MaxPlaintextSize+1), Binding{Purpose: "totp", RecordID: "id"})
	require.ErrorIs(t, err, ErrPlaintextTooLarge)
	require.Zero(t, keys.calls)
}

func TestEnvelopePropagatesKeyFailure(t *testing.T) {
	t.Parallel()
	binding := Binding{Purpose: "totp", RecordID: "id"}
	frame, err := ForUser(&keyManager{}).Seal(t.Context(), nil, binding)
	require.NoError(t, err)
	for _, tt := range []struct {
		name     string
		canceled bool
	}{
		{name: "provider failure"},
		{name: "cancellation", canceled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			failure := errors.New("key unavailable")
			keys := &keyManager{err: failure}
			enc := ForUser(keys)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
				failure = context.Canceled
			}
			sealed, err := enc.Seal(ctx, nil, binding)
			require.ErrorIs(t, err, failure)
			require.Nil(t, sealed)
			plaintext, err := enc.Open(ctx, frame, binding)
			require.ErrorIs(t, err, failure)
			require.Nil(t, plaintext)
			if tt.canceled {
				require.Zero(t, keys.calls)
			} else {
				require.Equal(t, make([]byte, 32), keys.returned)
			}
		})
	}
}

// All scopes deliberately share key bytes so authentication must enforce scope.
type sameEnvelopeKeys struct{ keyManager }

func (m *sameEnvelopeKeys) OrganizationKey(ctx context.Context, orgID string) ([]byte, error) {
	return m.resolve(ctx, orgID, 1)
}
