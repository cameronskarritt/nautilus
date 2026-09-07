package testutil

import (
	"bytes"
	"context"
	"testing"

	"nautilus/internal/crypto/encrypt"
)

// TestEncrypter returns a shared-user encrypter with a deterministic test key.
func TestEncrypter(t *testing.T) *encrypt.Encrypter {
	t.Helper()
	return encrypt.ForUser(encryptionKeys{})
}

func ContextWithEncrypter(t *testing.T) context.Context {
	t.Helper()
	return encrypt.WithContext(t.Context(), TestEncrypter(t))
}

type encryptionKeys struct{}

func (encryptionKeys) UserKey(context.Context) ([]byte, error) {
	return bytes.Repeat([]byte{1}, 32), nil
}

func (encryptionKeys) OrganizationKey(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{2}, 32), nil
}
