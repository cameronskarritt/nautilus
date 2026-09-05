package organizations

import (
	"context"
	"database/sql"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

func CreateMember(
	ctx context.Context,
	db database.Database,
	userID int,
	organizationID int,
	role Role,
	displayName optional.Optional[string],
) (*Member, error) {
	query := `
		INSERT INTO org_members(user_id, organization_id, role, display_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id;
	`

	var id int
	err := db.QueryRow(ctx, query, userID, organizationID, role, displayName).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create org member")
	}

	return GetMember(ctx, db, id)
}

// CreateOrRestoreMember creates a membership or restores and updates its role when a
// soft-deleted membership already exists.
func CreateOrRestoreMember(
	ctx context.Context,
	db database.Database,
	userID int,
	organizationID int,
	role Role,
) (*Member, error) {
	query := `
		INSERT INTO org_members(user_id, organization_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, organization_id) DO UPDATE SET
			role = EXCLUDED.role,
			updated_at = CURRENT_TIMESTAMP,
			deleted_at = NULL
		RETURNING id;
	`

	var id int
	err := db.QueryRow(ctx, query, userID, organizationID, role).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create or restore org member")
	}

	return GetMember(ctx, db, id)
}

func GetMember(ctx context.Context, db database.Database, id int) (*Member, error) {
	member := new(Member)

	query := `
		SELECT id, external_id, user_id, organization_id, role, display_name, permissions, created_at
		FROM org_members
		WHERE id = $1 AND deleted_at IS NULL;
	`

	err := db.QueryRow(ctx, query, id).Scan(
		&member.ID,
		&member.ExternalID,
		&member.UserID,
		&member.OrganizationID,
		&member.Role,
		&member.DisplayName,
		&member.Permissions,
		&member.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch org member")
	}

	return member, nil
}

func GetMemberByExternalID(ctx context.Context, db database.Database, externalID string) (*Member, error) {
	query := `SELECT id FROM org_members WHERE external_id = $1 AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, externalID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch org member by external ID")
	}

	return GetMember(ctx, db, id)
}

func GetMemberByUserAndOrg(ctx context.Context, db database.Database, userID int, organizationID int) (*Member, error) {
	query := `SELECT id FROM org_members WHERE user_id = $1 AND organization_id = $2 AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, userID, organizationID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch org member by user and org")
	}

	return GetMember(ctx, db, id)
}

func GetDefaultMemberForUser(ctx context.Context, db database.Database, userID int) (*Member, error) {
	// Returns the personal org membership first, or the most recently created membership
	query := `
		SELECT om.id
		FROM org_members om
		JOIN organizations o ON om.organization_id = o.id
		WHERE om.user_id = $1 AND om.deleted_at IS NULL AND o.deleted_at IS NULL
		ORDER BY o.personal DESC, om.created_at ASC
		LIMIT 1;
	`

	var id int
	err := db.QueryRow(ctx, query, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch default org member")
	}

	return GetMember(ctx, db, id)
}

func ListMembersByUser(ctx context.Context, db database.Database, userID int) ([]*Member, error) {
	query := `
		SELECT id, external_id, user_id, organization_id, role, display_name, permissions, created_at
		FROM org_members
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC;
	`

	rows, err := db.Query(ctx, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list org members by user")
	}

	var members []*Member
	err = database.ScanRows(rows, func(row database.Row) error {
		m := new(Member)
		if err := row.Scan(
			&m.ID,
			&m.ExternalID,
			&m.UserID,
			&m.OrganizationID,
			&m.Role,
			&m.DisplayName,
			&m.Permissions,
			&m.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan org member")
		}
		members = append(members, m)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

func ListMembersByOrg(ctx context.Context, db database.Database, organizationID int) ([]*Member, error) {
	query := `
		SELECT id, external_id, user_id, organization_id, role, display_name, permissions, created_at
		FROM org_members
		WHERE organization_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC;
	`

	rows, err := db.Query(ctx, query, organizationID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list org members by org")
	}

	var members []*Member
	err = database.ScanRows(rows, func(row database.Row) error {
		m := new(Member)
		if err := row.Scan(
			&m.ID,
			&m.ExternalID,
			&m.UserID,
			&m.OrganizationID,
			&m.Role,
			&m.DisplayName,
			&m.Permissions,
			&m.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan org member")
		}
		members = append(members, m)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

func UpdateMemberRole(ctx context.Context, db database.Database, id int, role Role) error {
	query := `UPDATE org_members SET role = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, role, id)
	if err != nil {
		return errors.Wrap(err, "unable to update org member role")
	}

	return nil
}

func UpdateMemberDisplayName(ctx context.Context, db database.Database, id int, displayName optional.Optional[string]) error {
	query := `UPDATE org_members SET display_name = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, displayName, id)
	if err != nil {
		return errors.Wrap(err, "unable to update org member display name")
	}

	return nil
}

func DeleteMember(ctx context.Context, db database.Database, id int) error {
	query := `UPDATE org_members SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "unable to delete org member")
	}

	return nil
}
