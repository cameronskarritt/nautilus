package organizations

import (
	"context"
	"database/sql"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

func Create(
	ctx context.Context,
	db database.Database,
	slug string,
	name string,
	personal bool,
	settings optional.Optional[Settings],
) (*Organization, error) {
	query := `
		INSERT INTO organizations(slug, name, personal, settings)
		VALUES ($1, $2, $3, $4)
		RETURNING id;
	`

	var id int
	err := db.QueryRow(ctx, query, slug, name, personal, settings).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create organization")
	}

	return Get(ctx, db, id)
}

func Get(ctx context.Context, db database.Database, id int) (*Organization, error) {
	org := new(Organization)

	query := `
		SELECT id, external_id, slug, name, plan, personal, settings, created_at
		FROM organizations
		WHERE id = $1 AND deleted_at IS NULL;
	`

	err := db.QueryRow(ctx, query, id).Scan(
		&org.ID,
		&org.ExternalID,
		&org.Slug,
		&org.Name,
		&org.Plan,
		&org.Personal,
		&org.Settings,
		&org.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch organization")
	}

	return org, nil
}

func GetByExternalID(ctx context.Context, db database.Database, externalID string) (*Organization, error) {
	query := `SELECT id FROM organizations WHERE external_id = $1 AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, externalID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch organization by external ID")
	}

	return Get(ctx, db, id)
}

func GetBySlug(ctx context.Context, db database.Database, slug string) (*Organization, error) {
	query := `SELECT id FROM organizations WHERE slug = $1 AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, slug).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch organization by slug")
	}

	return Get(ctx, db, id)
}

func GetPersonalByUserID(ctx context.Context, db database.Database, userID int) (*Organization, error) {
	query := `
		SELECT o.id
		FROM organizations o
		JOIN org_members om ON o.id = om.organization_id
		WHERE om.user_id = $1 AND o.personal = true AND o.deleted_at IS NULL AND om.deleted_at IS NULL;
	`

	var id int
	err := db.QueryRow(ctx, query, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch personal organization")
	}

	return Get(ctx, db, id)
}

func ListByUser(ctx context.Context, db database.Database, userID int) ([]*Organization, error) {
	query := `
		SELECT o.id, o.external_id, o.slug, o.name, o.plan, o.personal, o.settings, o.created_at
		FROM organizations o
		JOIN org_members om ON o.id = om.organization_id
		WHERE om.user_id = $1 AND o.deleted_at IS NULL AND om.deleted_at IS NULL
		ORDER BY o.personal DESC, o.created_at DESC;
	`

	rows, err := db.Query(ctx, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list organizations")
	}

	var orgs []*Organization
	err = database.ScanRows(rows, func(row database.Row) error {
		org := new(Organization)
		if err := row.Scan(
			&org.ID,
			&org.ExternalID,
			&org.Slug,
			&org.Name,
			&org.Plan,
			&org.Personal,
			&org.Settings,
			&org.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan organization")
		}
		orgs = append(orgs, org)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return orgs, nil
}

func ListAll(ctx context.Context, db database.Database) ([]*Organization, error) {
	query := `
		SELECT id, external_id, slug, name, plan, personal, settings, created_at
		FROM organizations
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC;
	`

	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list all organizations")
	}

	var orgs []*Organization
	err = database.ScanRows(rows, func(row database.Row) error {
		org := new(Organization)
		if err := row.Scan(
			&org.ID,
			&org.ExternalID,
			&org.Slug,
			&org.Name,
			&org.Plan,
			&org.Personal,
			&org.Settings,
			&org.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan organization")
		}
		orgs = append(orgs, org)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return orgs, nil
}

func Update(ctx context.Context, db database.Database, id int, name string) error {
	query := `UPDATE organizations SET name = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, name, id)
	if err != nil {
		return errors.Wrap(err, "unable to update organization")
	}

	return nil
}

func UpdatePlan(ctx context.Context, db database.Database, id int, plan string) error {
	query := `UPDATE organizations SET plan = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, plan, id)
	if err != nil {
		return errors.Wrap(err, "unable to update organization plan")
	}

	return nil
}

func Delete(ctx context.Context, db database.Database, id int) error {
	query := `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "unable to delete organization")
	}

	return nil
}
