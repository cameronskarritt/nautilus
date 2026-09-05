package testutil

import (
	"context"
	"testing"

	"nautilus/internal/crypto/encrypt"
)

// testKey is a 32-byte (64 hex char) key for testing.
const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// TestEncrypter returns an Encrypter configured with a test key.
func TestEncrypter(t *testing.T) *encrypt.Encrypter {
	t.Helper()
	enc, err := encrypt.NewEncrypter(testKey)
	if err != nil {
		t.Fatalf("failed to create test encrypter: %v", err)
	}
	return enc
}

// ContextWithEncrypter returns a context with a test Encrypter attached.
func ContextWithEncrypter(t *testing.T) context.Context {
	t.Helper()
	return encrypt.WithContext(context.Background(), TestEncrypter(t))
}
