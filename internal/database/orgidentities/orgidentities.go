package orgidentities

import (
	"context"
	"database/sql"

	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

func Create(
	ctx context.Context,
	db database.Database,
	organizationID int,
	provider enums.AuthProvider,
	providerID string,
) (*Identity, error) {
	query := `
		INSERT INTO organization_identities(organization_id, provider, provider_id)
		VALUES ($1, $2, $3)
		RETURNING id;
	`

	var id int
	err := db.QueryRow(ctx, query, organizationID, provider, providerID).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create organization identity")
	}

	return Get(ctx, db, id)
}

func Get(ctx context.Context, db database.Database, id int) (*Identity, error) {
	identity := new(Identity)
	query := `
		SELECT id, external_id, organization_id, provider, provider_id, created_at
		FROM organization_identities
		WHERE id = $1 AND deleted_at IS NULL;
	`

	err := db.QueryRow(ctx, query, id).Scan(
		&identity.ID,
		&identity.ExternalID,
		&identity.OrganizationID,
		&identity.Provider,
		&identity.ProviderID,
		&identity.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch organization identity")
	}

	return identity, nil
}

func GetByProvider(
	ctx context.Context,
	db database.Database,
	provider enums.AuthProvider,
	providerID string,
) (*Identity, error) {
	query := `
		SELECT id
		FROM organization_identities
		WHERE provider = $1 AND provider_id = $2 AND deleted_at IS NULL;
	`

	var id int
	err := db.QueryRow(ctx, query, provider, providerID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch organization identity by provider")
	}

	return Get(ctx, db, id)
}

// Ensure provisions an organization and membership for an identity. Callers
// should run it inside the same transaction as any related user creation.
func Ensure(
	ctx context.Context,
	db database.Database,
	userID int,
	provider enums.AuthProvider,
	providerID string,
	slug string,
	name string,
	role organizations.Role,
) (*organizations.Organization, *organizations.Member, error) {
	identity, err := GetByProvider(ctx, db, provider, providerID)
	if err != nil {
		return nil, nil, err
	}

	var org *organizations.Organization
	if identity == nil {
		org, err = organizations.Create(ctx, db, slug, name, false, optional.Empty[organizations.Settings]())
		if err != nil {
			return nil, nil, err
		}

		_, err = Create(ctx, db, org.ID, provider, providerID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		org, err = organizations.Get(ctx, db, identity.OrganizationID)
		if err != nil {
			return nil, nil, err
		}
		if org == nil {
			return nil, nil, errors.New("organization identity references a missing organization")
		}
		if org.Name != name {
			if err := organizations.Update(ctx, db, org.ID, name); err != nil {
				return nil, nil, err
			}
			org.Name = name
		}
	}

	member, err := organizations.CreateOrRestoreMember(ctx, db, userID, org.ID, role)
	if err != nil {
		return nil, nil, err
	}

	return org, member, nil
}
