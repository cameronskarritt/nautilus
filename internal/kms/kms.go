package kms

import "context"

// KeyManager resolves stable, raw 32-byte application encryption keys.
// Successful calls return caller-owned copies; callers must not log or persist
// plaintext keys. KMS-backed implementations unwrap application keys rather than
// export provider-managed KMS keys. Authorization is the caller's responsibility.
type KeyManager interface {
	// OrganizationKey returns the key for an organization's external ID.
	// Keys are distinct per organization and never fall back to the user key.
	OrganizationKey(ctx context.Context, orgID string) ([]byte, error)

	// UserKey returns the shared key for user secrets, independent of the active
	// organization and distinct from every organization key.
	UserKey(ctx context.Context) ([]byte, error)
}
