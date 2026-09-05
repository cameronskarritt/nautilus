package users

import (
	"context"
	"database/sql"
	"time"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/errors"
)

// PendingTOTP represents a pending TOTP setup.
type PendingTOTP struct {
	Secret    string
	ExpiresAt time.Time
}

// SetPendingTOTP stores a TOTP secret as pending for MFA setup.
// The secret is encrypted before storage using the Encrypter from context.
func SetPendingTOTP(ctx context.Context, db database.Database, userID int, secret string, expiresAt time.Time) error {
	enc := encrypt.FromContext(ctx)
	if enc == nil {
		return errors.New("encrypter not found in context")
	}

	encrypted, err := enc.Encrypt([]byte(secret))
	if err != nil {
		return errors.Wrap(err, "unable to encrypt TOTP secret")
	}

	query := `
		UPDATE users
		SET totp_secret = $1, totp_pending_at = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND mfa_enabled = false
	`
	_, err = db.Exec(ctx, query, encrypted, expiresAt, userID)
	if err != nil {
		return errors.Wrap(err, "unable to set pending TOTP secret")
	}
	return nil
}

// GetPendingTOTP retrieves the pending TOTP secret for a user.
// Returns nil if no pending secret exists, MFA is already enabled, or if it has expired.
func GetPendingTOTP(ctx context.Context, db database.Database, userID int) (*PendingTOTP, error) {
	query := `
		SELECT totp_secret, totp_pending_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL AND mfa_enabled = false
	`
	var encrypted []byte
	var pendingAt sql.NullTime
	err := db.QueryRow(ctx, query, userID).Scan(&encrypted, &pendingAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch pending TOTP")
	}

	if encrypted == nil || !pendingAt.Valid {
		return nil, nil
	}

	if time.Now().After(pendingAt.Time) {
		return nil, nil
	}

	enc := encrypt.FromContext(ctx)
	if enc == nil {
		return nil, errors.New("encrypter not found in context")
	}

	secret, err := enc.Decrypt(encrypted)
	if err != nil {
		return nil, errors.Wrap(err, "unable to decrypt TOTP secret")
	}

	return &PendingTOTP{
		Secret:    string(secret),
		ExpiresAt: pendingAt.Time,
	}, nil
}

// EnableMFA marks MFA as enabled (secret is already in totp_secret from pending).
func EnableMFA(ctx context.Context, db database.Database, userID int) error {
	query := `
		UPDATE users
		SET totp_pending_at = NULL,
			mfa_enabled = true,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`
	_, err := db.Exec(ctx, query, userID)
	if err != nil {
		return errors.Wrap(err, "unable to enable MFA")
	}
	return nil
}

// DisableMFA clears the TOTP secret and disables MFA.
func DisableMFA(ctx context.Context, db database.Database, userID int) error {
	query := `
		UPDATE users
		SET totp_secret = NULL,
			totp_pending_at = NULL,
			mfa_enabled = false,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`
	_, err := db.Exec(ctx, query, userID)
	if err != nil {
		return errors.Wrap(err, "unable to disable MFA")
	}
	return nil
}

// GetTOTPSecret retrieves the decrypted TOTP secret for a user.
// Returns empty string if MFA is not enabled.
func GetTOTPSecret(ctx context.Context, db database.Database, userID int) (string, error) {
	query := `
		SELECT totp_secret
		FROM users
		WHERE id = $1 AND deleted_at IS NULL AND mfa_enabled = true
	`
	var encrypted []byte
	err := db.QueryRow(ctx, query, userID).Scan(&encrypted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", errors.Wrap(err, "unable to fetch TOTP secret")
	}

	if encrypted == nil {
		return "", nil
	}

	enc := encrypt.FromContext(ctx)
	if enc == nil {
		return "", errors.New("encrypter not found in context")
	}

	secret, err := enc.Decrypt(encrypted)
	if err != nil {
		return "", errors.Wrap(err, "unable to decrypt TOTP secret")
	}

	return string(secret), nil
}

// HasMFAEnabled checks if a user has MFA enabled.
func HasMFAEnabled(ctx context.Context, db database.Database, userID int) (bool, error) {
	query := `
		SELECT mfa_enabled
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`
	var enabled bool
	err := db.QueryRow(ctx, query, userID).Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.Wrap(err, "unable to check MFA status")
	}
	return enabled, nil
}
