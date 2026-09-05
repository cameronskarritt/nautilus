package agentstreams

import (
	"context"
	"database/sql"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/pagination"
	"nautilus/internal/querybuilder"
)

var ErrFenceViolation = errors.New("agent stream fence token mismatch")

// Create creates a new agent stream owned by the given user and organization.
func Create(ctx context.Context, db database.Database, userID, orgID int) (*Stream, error) {
	query := `
		INSERT INTO agent_streams(user_id, org_id, status)
		VALUES ($1, $2, $3)
		RETURNING id;
	`

	var id int
	err := db.QueryRow(ctx, query, userID, orgID, StatusPending).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create agent stream")
	}

	return Get(ctx, db, id)
}

// Get retrieves a stream by its internal ID.
func Get(ctx context.Context, db database.Database, id int) (*Stream, error) {
	stream := new(Stream)

	query := `
		SELECT id, external_id, user_id, org_id, status, fence_token, title, created_at, updated_at
		FROM agent_streams
		WHERE id = $1;
	`

	err := db.QueryRow(ctx, query, id).Scan(
		&stream.ID,
		&stream.ExternalID,
		&stream.UserID,
		&stream.OrgID,
		&stream.Status,
		&stream.FenceToken,
		&stream.Title,
		&stream.CreatedAt,
		&stream.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent stream")
	}

	return stream, nil
}

// GetByExternalID retrieves a stream by its external UUID.
func GetByExternalID(ctx context.Context, db database.Database, externalID string) (*Stream, error) {
	stream := new(Stream)

	query := `
		SELECT id, external_id, user_id, org_id, status, fence_token, title, created_at, updated_at
		FROM agent_streams
		WHERE external_id = $1;
	`

	err := db.QueryRow(ctx, query, externalID).Scan(
		&stream.ID,
		&stream.ExternalID,
		&stream.UserID,
		&stream.OrgID,
		&stream.Status,
		&stream.FenceToken,
		&stream.Title,
		&stream.CreatedAt,
		&stream.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent stream")
	}

	return stream, nil
}

func GetByExternalIDForOrganization(
	ctx context.Context,
	db database.Database,
	organizationID int,
	externalID string,
) (*Stream, error) {
	stream := new(Stream)
	query := `
		SELECT id, external_id, user_id, org_id, status, fence_token, title, created_at, updated_at
		FROM agent_streams
		WHERE org_id = $1 AND external_id = $2;
	`
	err := db.QueryRow(ctx, query, organizationID, externalID).Scan(
		&stream.ID,
		&stream.ExternalID,
		&stream.UserID,
		&stream.OrgID,
		&stream.Status,
		&stream.FenceToken,
		&stream.Title,
		&stream.CreatedAt,
		&stream.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent stream for organization")
	}
	return stream, nil
}

// ProjectStatus updates a stream's runtime status if the caller still owns its fence.
func ProjectStatus(ctx context.Context, db database.Database, id int, fenceToken int64, status Status) error {
	query := `
		UPDATE agent_streams
		SET status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND fence_token = $3;
	`

	result, err := db.Exec(ctx, query, status, id, fenceToken)
	if err != nil {
		return errors.Wrap(err, "unable to update stream status")
	}
	if result.RowsAffected() == 0 {
		return ErrFenceViolation
	}

	return nil
}

// SetTitle sets the title (preview) of a stream.
func SetTitle(ctx context.Context, db database.Database, id int, title string) error {
	query := `
		UPDATE agent_streams
		SET title = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2;
	`

	_, err := db.Exec(ctx, query, title, id)
	if err != nil {
		return errors.Wrap(err, "unable to set stream title")
	}

	return nil
}

// AcquireFence atomically increments the fence token for a stream and returns the new value.
// This should be called when starting or resuming an agent instance for the stream.
func AcquireFence(ctx context.Context, db database.Database, id int) (int64, error) {
	query := `
		UPDATE agent_streams
		SET fence_token = fence_token + 1, status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		RETURNING fence_token;
	`

	var fenceToken int64
	err := db.QueryRow(ctx, query, StatusRunning, id).Scan(&fenceToken)
	if err != nil {
		return 0, errors.Wrap(err, "unable to acquire fence token")
	}

	return fenceToken, nil
}

// List retrieves a page of streams ordered by most recently updated.
//
// Pagination is keyset-based on (updated_at DESC, id ASC).
func List(
	ctx context.Context,
	db database.Database,
	organizationID int,
	params pagination.Params,
) (pagination.Page[*Stream], error) {
	query, args, err := querybuilder.
		Select("id", "external_id", "user_id", "org_id", "status", "fence_token", "title", "created_at", "updated_at").
		From("agent_streams").
		Where(querybuilder.Eq{"org_id": organizationID}).
		OrderBy(
			querybuilder.Desc, "updated_at",
			querybuilder.Asc, "id",
		).
		Paginate(params).
		Build()
	if err != nil {
		return pagination.Page[*Stream]{}, errors.Wrap(err, "unable to build agent streams query")
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return pagination.Page[*Stream]{}, errors.Wrap(err, "unable to list agent streams")
	}

	var streams []*Stream
	err = database.ScanRows(rows, func(row database.Row) error {
		s := new(Stream)
		if err := row.Scan(
			&s.ID,
			&s.ExternalID,
			&s.UserID,
			&s.OrgID,
			&s.Status,
			&s.FenceToken,
			&s.Title,
			&s.CreatedAt,
			&s.UpdatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan agent stream")
		}

		streams = append(streams, s)
		return nil
	})
	if err != nil {
		return pagination.Page[*Stream]{}, err
	}

	page := pagination.Build(streams, params.Limit, func(s *Stream) pagination.Cursor {
		return pagination.Cursor{
			"updated_at": s.UpdatedAt,
			"id":         s.ID,
		}
	})
	if page.Data == nil {
		page.Data = []*Stream{}
	}
	return page, nil
}
