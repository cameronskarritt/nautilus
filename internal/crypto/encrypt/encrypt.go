package encrypt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/kms"
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
	key func(context.Context) ([]byte, error)
}

// ForUser resolves the shared user key only when encrypting or decrypting.
func ForUser(keys kms.KeyManager) *Encrypter {
	if keys == nil {
		return new(Encrypter)
	}
	return &Encrypter{key: keys.UserKey}
}

// ForOrganization binds key lookup to an organization, never the shared user key.
func ForOrganization(keys kms.KeyManager, orgID string) *Encrypter {
	if keys == nil || orgID == "" {
		return new(Encrypter)
	}
	return &Encrypter{key: func(ctx context.Context) ([]byte, error) {
		return keys.OrganizationKey(ctx, orgID)
	}}
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
	gcm, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	return &Encrypter{gcm: gcm}, nil
}

func newCipher(key []byte) (cipher.AEAD, error) {
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

	return gcm, nil
}

func (e *Encrypter) cipher(ctx context.Context) (cipher.AEAD, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "unable to resolve encryption key")
	}
	if e.gcm != nil {
		return e.gcm, nil
	}
	if e.key == nil {
		return nil, errors.New("encrypter has no key source")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	key, err := e.key(ctx)
	defer clear(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "unable to resolve encryption key")
	}
	return newCipher(key)
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns nonce (12 bytes) prepended to the ciphertext.
func (e *Encrypter) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	gcm, err := e.cipher(ctx)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.Wrap(err, "failed to generate nonce")
	}

	// Seal appends the ciphertext to nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
// Expects the nonce to be prepended to the ciphertext.
func (e *Encrypter) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	// The stored format includes a 12-byte nonce and a 16-byte GCM tag.
	if len(ciphertext) < 12+16 {
		return nil, errors.New("ciphertext too short")
	}
	gcm, err := e.cipher(ctx)
	if err != nil {
		return nil, err
	}

	nonce := ciphertext[:gcm.NonceSize()]
	ciphertext = ciphertext[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decrypt")
	}

	return plaintext, nil
}
