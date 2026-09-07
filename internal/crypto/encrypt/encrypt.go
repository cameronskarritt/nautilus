package encrypt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"sync"

	"nautilus/internal/config"
	"nautilus/internal/errors"
)

type contextKey struct{}

// WithContext returns a new context with the Encrypter attached.
func WithContext(ctx context.Context, enc *Encrypter) context.Context {
	return context.WithValue(ctx, contextKey{}, enc)
}

// FromContext retrieves the Encrypter from the context.
// Returns nil if no Encrypter is present.
func FromContext(ctx context.Context) *Encrypter {
	enc, _ := ctx.Value(contextKey{}).(*Encrypter)
	return enc
}

// Encrypter provides AES-256-GCM encryption/decryption.
type Encrypter struct {
	gcm cipher.AEAD
}

// NewEncrypter creates an Encrypter from a hex-encoded or base64-encoded 32-byte key.
// It automatically detects the encoding format.
func NewEncrypter(keyStr string) (*Encrypter, error) {
	var key []byte
	var err error
	var encoding string

	// Try base64 first (more common for dotenvx decrypted values)
	key, err = base64.StdEncoding.DecodeString(keyStr)
	if err == nil && len(key) == 32 {
		encoding = "base64"
	} else {
		base64Err := err
		// Try hex as fallback
		key, err = hex.DecodeString(keyStr)
		if err == nil && len(key) == 32 {
			encoding = "hex"
		} else {
			return nil, errors.Errorf("failed to decode key: tried base64 and hex, neither produced a 32-byte key (base64 err: %v, hex err: %v, decoded length: %d)", base64Err, err, len(key))
		}
	}

	if len(key) != 32 {
		return nil, errors.Errorf("key must be 32 bytes after decoding (got %d bytes from %s)", len(key), encoding)
	}

	return New(key)
}

// New creates an Encrypter from a raw 32-byte key.
func New(key []byte) (*Encrypter, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create AES cipher")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create GCM cipher")
	}

	return &Encrypter{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns nonce (12 bytes) prepended to the ciphertext.
func (e *Encrypter) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.Wrap(err, "failed to generate nonce")
	}

	// Seal appends the ciphertext to nonce
	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
// Expects the nonce to be prepended to the ciphertext.
func (e *Encrypter) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < e.gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:e.gcm.NonceSize()]
	ciphertext = ciphertext[e.gcm.NonceSize():]

	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decrypt")
	}

	return plaintext, nil
}

// Package-level functions for backward compatibility.
// These use a lazily-initialized default encrypter from the ENCRYPTION_KEY env var.

var (
	defaultEnc     *Encrypter
	defaultEncOnce sync.Once
	defaultEncErr  error
)

func initDefault() {
	defaultEncOnce.Do(func() {
		key := config.Get[string]("ENCRYPTION_KEY")
		if key == "" {
			defaultEncErr = errors.New("ENCRYPTION_KEY environment variable is not set")
			return
		}

		defaultEnc, defaultEncErr = NewEncrypter(key)
		if defaultEncErr != nil {
			return
		}
	})
}

func Encrypt(plaintext []byte) ([]byte, error) {
	initDefault()
	if defaultEncErr != nil {
		return nil, defaultEncErr
	}
	return defaultEnc.Encrypt(plaintext)
}

func Decrypt(ciphertext []byte) ([]byte, error) {
	initDefault()
	if defaultEncErr != nil {
		return nil, defaultEncErr
	}
	return defaultEnc.Decrypt(ciphertext)
}

func DefaultEncrypter() *Encrypter {
	initDefault()
	return defaultEnc
}
