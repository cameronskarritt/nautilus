package authcodes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var b64 = base64.RawURLEncoding

func Create(
	ctx context.Context,
	db database.Database,
	codeType enums.AuthCodeType,
	userID int,
	expiration time.Duration,
	data any,
) (string, error) {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return "", errors.Wrap(err, "unable to read random source")
	}

	// Encode external token as hex (because I think it's prettier) :)
	token := hex.EncodeToString(buf)

	h := sha256.New()
	_, err = h.Write(buf)
	if err != nil {
		return "", errors.Wrap(err, "unable to hash token")
	}
	hash := h.Sum(nil)

	encoded := b64.EncodeToString(hash)
	expiresAt := time.Now().Add(expiration)

	var tokenData []byte
	if data != nil {
		tokenData, err = json.Marshal(data)
		if err != nil {
			return "", errors.Wrap(err, "unable to encode token data")
		}
	}

	err = database.Transact(ctx, db, func(txn database.Database) error {
		deactivate := `
			UPDATE auth_codes
			SET deleted_at = CURRENT_TIMESTAMP
			WHERE deleted_at IS NULL AND user_id = $1 AND type = $2;
		`
		_, err := txn.Exec(ctx, deactivate, userID, codeType)
		if err != nil {
			return errors.Wrap(err, "unable to deactivate past auth codes")
		}

		insert := `
			INSERT INTO auth_codes(user_id, type, token_hash, expires_at, data)
			VALUES($1, $2, $3, $4, $5);
		`
		_, err = txn.Exec(ctx, insert, userID, codeType, encoded, expiresAt, tokenData)
		if err != nil {
			return errors.Wrap(err, "unable to create auth code")
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return token, nil
}

func Verify(ctx context.Context, db database.Database, codeType enums.AuthCodeType, token string) (*AuthCode, error) {
	b, err := hex.DecodeString(token)
	if err != nil {
		// Return no error, handle these as invalid auth tokens
		return nil, nil //nolint:nilerr
	}

	h := sha256.New()
	_, err = h.Write(b)
	if err != nil {
		return nil, errors.Wrap(err, "unable to hash token")
	}
	hash := h.Sum(nil)

	encoded := b64.EncodeToString(hash)

	var authCode AuthCode
	var expiresAt time.Time

	query := `
		UPDATE auth_codes
		SET deleted_at = CURRENT_TIMESTAMP
		WHERE deleted_at IS NULL AND type = $1 AND token_hash = $2
		RETURNING user_id, data, expires_at;
	`
	err = db.QueryRow(ctx, query, codeType, encoded).Scan(&authCode.UserID, &authCode.Data, &expiresAt)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch auth code")
	}

	if time.Now().After(expiresAt) {
		return nil, nil
	}

	return &authCode, nil
}
