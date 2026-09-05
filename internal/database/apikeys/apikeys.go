package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

const (
	tokenPrefix        = "nautilus_"
	tokenBytes         = 32
	displayPrefixBytes = len(tokenPrefix) + 8
)

func Create(
	ctx context.Context,
	db database.Database,
	organizationID int,
	createdBy int,
	options *CreateOptions,
) (*Key, string, error) {
	name, scopes, err := normalizeCreateOptions(organizationID, createdBy, options)
	if err != nil {
		return nil, "", err
	}
	token, err := newToken()
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256([]byte(token))

	key, err := scanKey(db.QueryRow(ctx, `
		INSERT INTO api_keys(
			organization_id, created_by, name, token_hash, prefix, scopes
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, external_id, organization_id, created_by, name, prefix,
			scopes, created_at, updated_at;
	`, organizationID, createdBy, name, hash[:], token[:displayPrefixBytes], scopes))
	if err != nil {
		return nil, "", errors.Wrap(err, "unable to create API key")
	}
	return key, token, nil
}

func List(
	ctx context.Context,
	db database.Database,
	organizationID int,
) ([]*Key, error) {
	rows, err := db.Query(ctx, `
		SELECT id, external_id, organization_id, created_by, name, prefix,
			scopes, created_at, updated_at
		FROM api_keys
		WHERE organization_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC;
	`, organizationID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list API keys")
	}

	keys := make([]*Key, 0)
	err = database.ScanRows(rows, func(row database.Row) error {
		key, err := scanKey(row)
		if err != nil {
			return errors.Wrap(err, "unable to scan API key")
		}
		keys = append(keys, key)
		return nil
	})
	return keys, err
}

func RevokeByExternalID(
	ctx context.Context,
	db database.Database,
	organizationID int,
	externalID string,
) (bool, error) {
	result, err := db.Exec(ctx, `
		UPDATE api_keys
		SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE organization_id = $1 AND external_id = $2
			AND deleted_at IS NULL;
	`, organizationID, externalID)
	if err != nil {
		return false, errors.Wrap(err, "unable to revoke API key")
	}
	return result.RowsAffected() == 1, nil
}

// Authenticate is intentionally not organization-scoped because the token's
// organization is unknown until the credential is resolved.
func Authenticate(ctx context.Context, db database.Database, token string) (*Key, error) {
	if !validToken(token) {
		return nil, nil
	}
	hash := sha256.Sum256([]byte(token))
	key, err := scanKey(db.QueryRow(ctx, `
		SELECT id, external_id, organization_id, created_by, name, prefix,
			scopes, created_at, updated_at
		FROM api_keys
		WHERE token_hash = $1 AND deleted_at IS NULL;
	`, hash[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to authenticate API key")
	}
	return key, nil
}

func normalizeCreateOptions(
	organizationID int,
	createdBy int,
	options *CreateOptions,
) (string, []string, error) {
	if organizationID <= 0 || createdBy <= 0 || options == nil {
		return "", nil, errors.New("API key organization, creator, and options are required")
	}
	name := strings.TrimSpace(options.Name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
		return "", nil, errors.New("API key name must contain 1 to 100 characters")
	}

	seen := make(map[Scope]bool, len(options.Scopes))
	for _, scope := range options.Scopes {
		if !scope.IsValid() {
			return "", nil, errors.New("API key scope is invalid")
		}
		seen[scope] = true
	}
	if len(seen) == 0 {
		return "", nil, errors.New("at least one API key scope is required")
	}

	scopes := make([]string, 0, len(seen))
	for _, scope := range []Scope{ScopeRead, ScopeWrite} {
		if seen[scope] {
			scopes = append(scopes, string(scope))
		}
	}
	return name, scopes, nil
}

func newToken() (string, error) {
	data := make([]byte, tokenBytes)
	if _, err := rand.Read(data); err != nil {
		return "", errors.Wrap(err, "unable to generate API key")
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func validToken(token string) bool {
	if !strings.HasPrefix(token, tokenPrefix) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, tokenPrefix))
	return err == nil && len(data) == tokenBytes
}

func scanKey(row database.Row) (*Key, error) {
	key := new(Key)
	var scopes []string
	err := row.Scan(
		&key.ID,
		&key.ExternalID,
		&key.OrganizationID,
		&key.CreatedBy,
		&key.Name,
		&key.Prefix,
		&scopes,
		&key.CreatedAt,
		&key.UpdatedAt,
	)
	key.Scopes = make([]Scope, len(scopes))
	for index, scope := range scopes {
		key.Scopes[index] = Scope(scope)
	}
	return key, err
}
