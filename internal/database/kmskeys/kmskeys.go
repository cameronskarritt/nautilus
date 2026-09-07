package kmskeys

import (
	"context"
	"database/sql"
	"strings"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

func GetOrganization(ctx context.Context, db database.Database, orgID int) (*Key, error) {
	if orgID <= 0 {
		return nil, errors.New("invalid organization ID")
	}

	query := `
		SELECT k.id, k.organization_id, k.provider_key_id, k.ciphertext, k.created_at
		FROM kms_keys k
		JOIN organizations o ON o.id = k.organization_id
		WHERE k.organization_id = $1 AND o.deleted_at IS NULL;
	`
	return scan(db.QueryRow(ctx, query, orgID))
}

func GetUser(ctx context.Context, db database.Database) (*Key, error) {
	query := `
		SELECT id, organization_id, provider_key_id, ciphertext, created_at
		FROM kms_keys WHERE organization_id IS NULL;
	`
	return scan(db.QueryRow(ctx, query))
}

func CreateOrganization(ctx context.Context, db database.Database, orgID int, providerKeyID string, ciphertext []byte) (*Key, error) {
	if orgID <= 0 {
		return nil, errors.New("invalid organization ID")
	}
	if err := validate(providerKeyID, ciphertext); err != nil {
		return nil, err
	}

	query := `
		INSERT INTO kms_keys(organization_id, provider_key_id, ciphertext)
		SELECT id, $2, $3 FROM organizations WHERE id = $1 AND deleted_at IS NULL
		ON CONFLICT DO NOTHING;
	`
	if _, err := db.Exec(ctx, query, orgID, providerKeyID, ciphertext); err != nil {
		return nil, errors.Wrap(err, "unable to create organization key")
	}

	// A separate statement sees the committed winner after a concurrent insert.
	key, err := GetOrganization(ctx, db, orgID)
	if err != nil || key != nil {
		return key, err
	}
	var exists bool
	err = db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)", orgID).Scan(&exists)
	if err != nil {
		return nil, errors.Wrap(err, "unable to check key organization")
	}
	if !exists {
		return nil, nil
	}
	return nil, errors.New("provider key is already assigned to another scope")
}

func CreateUser(ctx context.Context, db database.Database, providerKeyID string, ciphertext []byte) (*Key, error) {
	if err := validate(providerKeyID, ciphertext); err != nil {
		return nil, err
	}

	query := `
		INSERT INTO kms_keys(provider_key_id, ciphertext) VALUES ($1, $2)
		ON CONFLICT DO NOTHING;
	`
	if _, err := db.Exec(ctx, query, providerKeyID, ciphertext); err != nil {
		return nil, errors.Wrap(err, "unable to create shared user key")
	}
	key, err := GetUser(ctx, db)
	if err != nil || key != nil {
		return key, err
	}
	return nil, errors.New("provider key is already assigned to another scope")
}

func validate(providerKeyID string, ciphertext []byte) error {
	if strings.TrimSpace(providerKeyID) == "" {
		return errors.New("provider key ID is required")
	}
	if len(ciphertext) == 0 {
		return errors.New("key ciphertext is required")
	}
	return nil
}

func scan(row database.Row) (*Key, error) {
	key := new(Key)
	if err := row.Scan(&key.ID, &key.OrganizationID, &key.ProviderKeyID, &key.Ciphertext, &key.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch wrapped key")
	}
	return key, nil
}
