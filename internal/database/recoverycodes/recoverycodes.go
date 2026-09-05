package recoverycodes

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"strings"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

const (
	// CodeCount is the number of recovery codes to generate
	CodeCount = 10
	// CodeLength is the length of each recovery code in bytes (before encoding)
	CodeLength = 5
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// hashCode returns the SHA-512 hash of a recovery code as a hex string.
func hashCode(code string) string {
	h := sha512.Sum512([]byte(code))
	return hex.EncodeToString(h[:])
}

// Generate creates a set of recovery codes for a user.
// It deletes any existing codes and returns the plaintext codes (shown once to user).
func Generate(ctx context.Context, db database.Database, userID int) ([]string, error) {
	codes := make([]string, CodeCount)

	// Generate random codes
	for i := range CodeCount {
		buf := make([]byte, CodeLength)
		_, err := rand.Read(buf)
		if err != nil {
			return nil, errors.Wrap(err, "unable to read random source")
		}
		// Encode as base32 for readability (e.g., "ABCD1234")
		codes[i] = b32.EncodeToString(buf)
	}

	err := database.Transact(ctx, db, func(txn database.Database) error {
		// Delete existing codes
		deleteQuery := `
			UPDATE mfa_recovery_codes
			SET deleted_at = CURRENT_TIMESTAMP
			WHERE user_id = $1 AND deleted_at IS NULL
		`
		_, err := txn.Exec(ctx, deleteQuery, userID)
		if err != nil {
			return errors.Wrap(err, "unable to delete existing recovery codes")
		}

		// Insert new codes
		insertQuery := `
			INSERT INTO mfa_recovery_codes (user_id, code_hash)
			VALUES ($1, $2)
		`
		for _, code := range codes {
			hash := hashCode(code)
			_, err = txn.Exec(ctx, insertQuery, userID, hash)
			if err != nil {
				return errors.Wrap(err, "unable to insert recovery code")
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return codes, nil
}

// Verify checks if the provided code is valid for the user.
// If valid, it marks the code as used and returns true.
// Recovery codes are case-insensitive.
func Verify(ctx context.Context, db database.Database, userID int, code string) (bool, error) {
	// Normalize code (remove spaces, uppercase)
	code = strings.ToUpper(strings.ReplaceAll(code, " ", ""))
	codeHash := hashCode(code)

	// Get all unused codes for this user
	query := `
		SELECT id, code_hash
		FROM mfa_recovery_codes
		WHERE user_id = $1 AND used_at IS NULL AND deleted_at IS NULL
	`
	rows, err := db.Query(ctx, query, userID)
	if err != nil {
		return false, errors.Wrap(err, "unable to query recovery codes")
	}

	var matchedID int
	err = database.ScanRows(rows, func(row database.Row) error {
		var id int
		var hash string
		if err := row.Scan(&id, &hash); err != nil {
			return errors.Wrap(err, "unable to scan recovery code")
		}

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(codeHash), []byte(hash)) == 1 {
			matchedID = id
		}
		return nil
	})
	if err != nil {
		return false, err
	}

	if matchedID == 0 {
		return false, nil
	}

	// Mark the code as used
	updateQuery := `
		UPDATE mfa_recovery_codes
		SET used_at = $1
		WHERE id = $2
	`
	_, err = db.Exec(ctx, updateQuery, time.Now(), matchedID)
	if err != nil {
		return false, errors.Wrap(err, "unable to mark recovery code as used")
	}

	return true, nil
}

// DeleteAll removes all recovery codes for a user (used when disabling MFA).
func DeleteAll(ctx context.Context, db database.Database, userID int) error {
	query := `
		UPDATE mfa_recovery_codes
		SET deleted_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND deleted_at IS NULL
	`
	_, err := db.Exec(ctx, query, userID)
	if err != nil {
		return errors.Wrap(err, "unable to delete recovery codes")
	}
	return nil
}

// CountRemaining returns the number of unused recovery codes for a user.
func CountRemaining(ctx context.Context, db database.Database, userID int) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM mfa_recovery_codes
		WHERE user_id = $1 AND used_at IS NULL AND deleted_at IS NULL
	`
	var count int
	err := db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, "unable to count recovery codes")
	}
	return count, nil
}
