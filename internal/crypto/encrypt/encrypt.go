package encrypt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
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

// Encrypter provides scoped AES-256-GCM envelope encryption.
type Encrypter struct {
	key   func(context.Context) ([]byte, error)
	scope string
}

// IsUser reports whether this handle can store shared-user secrets such as TOTP.
func (e *Encrypter) IsUser() bool {
	return e != nil && e.scope == "users"
}

// ForUser resolves the shared user key only when encrypting or decrypting.
func ForUser(keys kms.KeyManager) *Encrypter {
	if keys == nil {
		return new(Encrypter)
	}
	return &Encrypter{key: keys.UserKey, scope: "users"}
}

// ForOrganization binds key lookup to an organization, never the shared user key.
func ForOrganization(keys kms.KeyManager, orgID string) *Encrypter {
	if keys == nil || orgID == "" {
		return new(Encrypter)
	}
	return &Encrypter{scope: "organization:" + orgID, key: func(ctx context.Context) ([]byte, error) {
		return keys.OrganizationKey(ctx, orgID)
	}}
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
