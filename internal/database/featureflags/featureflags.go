package featureflags

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/featureflags"
	"nautilus/internal/log"
	"nautilus/internal/optional"
	"nautilus/internal/querybuilder"
)

// ErrFlagNameExists is returned when attempting to create a flag with a name that already exists.
var ErrFlagNameExists = errors.New("feature flag name already exists")

type dbFeatureFlagger struct {
	db database.Database
}

// FeatureFlag represents a feature flag record
type FeatureFlag struct {
	ID                int                       `json:"id"`
	Name              string                    `json:"name"`
	Description       optional.Optional[string] `json:"description"`
	Enabled           bool                      `json:"enabled"`
	RolloutPercentage float64                   `json:"rollout_percentage"`
	CreatedByID       optional.Optional[int]    `json:"-"`
	CreatedBy         *FlagCreator              `json:"created_by"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}

// FlagCreator represents the user who created a feature flag
type FlagCreator struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

func FeatureFlagger(db database.Database) featureflags.FeatureFlagger {
	return &dbFeatureFlagger{db: db}
}

func (f *dbFeatureFlagger) IsEnabled(
	ctx context.Context,
	objectType enums.FeatureFlagObjectType,
	objectID int,
	featureFlag featureflags.Flag,
) bool {
	logger := log.FromContext(ctx)

	// TODO(CLS): If we want to support rollout percentages, we can add a hash based on the
	// objectID and featureFlag here to roll against the percentage

	query := `
		SELECT enabled FROM feature_flags JOIN feature_flag_associations
		ON feature_flags.id = feature_flag_associations.feature_flag_id
		WHERE feature_flags.name = $1
		AND feature_flag_associations.object_id = $2
		AND feature_flag_associations.object_type = $3
		AND feature_flags.deleted_at IS NULL
		AND feature_flag_associations.deleted_at IS NULL;
	`
	row := f.db.QueryRow(ctx, query, featureFlag, objectID, objectType)

	var enabled bool
	err := row.Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false
		}

		logger.Error("error checking feature flag", "error", err)
		return false
	}
	return enabled
}

// ListAll returns all feature flags in the system.
func ListAll(ctx context.Context, db database.Database) ([]*FeatureFlag, error) {
	query := `
		SELECT id, name, description, enabled, rollout_percentage, created_by, created_at, updated_at
		FROM feature_flags
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC;
	`

	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list feature flags")
	}

	flags := make([]*FeatureFlag, 0)
	err = database.ScanRows(rows, func(row database.Row) error {
		flag := new(FeatureFlag)
		if err := row.Scan(
			&flag.ID,
			&flag.Name,
			&flag.Description,
			&flag.Enabled,
			&flag.RolloutPercentage,
			&flag.CreatedByID,
			&flag.CreatedAt,
			&flag.UpdatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan feature flag")
		}
		flags = append(flags, flag)
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to scan feature flags")
	}

	return flags, nil
}

// Create creates a new feature flag.
func Create(
	ctx context.Context,
	db database.Database,
	name string,
	description string,
	enabled bool,
) (*FeatureFlag, error) {
	user := users.FromContext(ctx)
	var createdBy *int
	if user != nil {
		createdBy = &user.ID
	}

	var desc optional.Optional[string]
	if description != "" {
		desc = optional.Set(description)
	}

	query := `
		INSERT INTO feature_flags(name, description, enabled, rollout_percentage, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, description, enabled, rollout_percentage, created_at, updated_at;
	`

	flag := new(FeatureFlag)
	err := db.QueryRow(ctx, query, name, desc, enabled, 1.0, createdBy).Scan(
		&flag.ID,
		&flag.Name,
		&flag.Description,
		&flag.Enabled,
		&flag.RolloutPercentage,
		&flag.CreatedAt,
		&flag.UpdatedAt,
	)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, ErrFlagNameExists
		}
		return nil, errors.Wrap(err, "unable to create feature flag")
	}

	return flag, nil
}

// UpdateOptions contains optional fields for updating a feature flag.
type UpdateOptions struct {
	Description optional.Optional[string]
	Enabled     optional.Optional[bool]
}

// Get retrieves a feature flag by its ID.
func Get(ctx context.Context, db database.Database, id int) (*FeatureFlag, error) {
	query := `
		SELECT id, name, description, enabled, rollout_percentage, created_at, updated_at
		FROM feature_flags
		WHERE id = $1 AND deleted_at IS NULL;
	`

	flag := new(FeatureFlag)
	err := db.QueryRow(ctx, query, id).Scan(
		&flag.ID,
		&flag.Name,
		&flag.Description,
		&flag.Enabled,
		&flag.RolloutPercentage,
		&flag.CreatedAt,
		&flag.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to get feature flag")
	}

	return flag, nil
}

// Update updates a feature flag by ID.
func Update(
	ctx context.Context,
	db database.Database,
	id int,
	opts UpdateOptions,
) (*FeatureFlag, error) {
	query, args, err := querybuilder.Update("feature_flags").
		Set(
			"description", opts.Description,
			"enabled", opts.Enabled,
			"updated_at", querybuilder.Expr("CURRENT_TIMESTAMP"),
		).
		Where(querybuilder.Eq{
			"id":         id,
			"deleted_at": nil,
		}).
		Build()
	if err != nil {
		return nil, err
	}

	// Add RETURNING clause
	query = strings.TrimSuffix(query, ";") +
		" RETURNING id, name, description, enabled, rollout_percentage, created_at, updated_at;"

	flag := new(FeatureFlag)
	err = db.QueryRow(ctx, query, args...).Scan(
		&flag.ID,
		&flag.Name,
		&flag.Description,
		&flag.Enabled,
		&flag.RolloutPercentage,
		&flag.CreatedAt,
		&flag.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to update feature flag")
	}

	return flag, nil
}

// Delete soft-deletes a feature flag by ID.
func Delete(ctx context.Context, db database.Database, id int) error {
	query := `
		UPDATE feature_flags
		SET deleted_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND deleted_at IS NULL;
	`

	result, err := db.Exec(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "unable to delete feature flag")
	}

	if result.RowsAffected() == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// List returns all feature flags associated with an object (user or organization)
// as a map where the key is the flag name and the value is the enabled state.
func (f *dbFeatureFlagger) List(
	ctx context.Context,
	objectType enums.FeatureFlagObjectType,
	objectID int,
) (map[string]bool, error) {
	query := `
		SELECT feature_flags.name, feature_flags.enabled
		FROM feature_flags
		JOIN feature_flag_associations
			ON feature_flags.id = feature_flag_associations.feature_flag_id
		WHERE feature_flag_associations.object_id = $1
			AND feature_flag_associations.object_type = $2
			AND feature_flags.deleted_at IS NULL
			AND feature_flag_associations.deleted_at IS NULL;
	`

	rows, err := f.db.Query(ctx, query, objectID, objectType)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list feature flags for object")
	}

	flags := make(map[string]bool)
	err = database.ScanRows(rows, func(row database.Row) error {
		var name string
		var enabled bool
		if err := row.Scan(&name, &enabled); err != nil {
			return errors.Wrap(err, "unable to scan feature flag")
		}
		flags[name] = enabled
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to scan feature flags")
	}

	return flags, nil
}

// ListAssociatedFlagIDs returns the IDs of flags associated with an object (user or organization).
func ListAssociatedFlagIDs(
	ctx context.Context,
	db database.Database,
	objectType enums.FeatureFlagObjectType,
	objectID int,
) ([]int, error) {
	query := `
		SELECT feature_flag_associations.feature_flag_id
		FROM feature_flag_associations
		JOIN feature_flags ON feature_flags.id = feature_flag_associations.feature_flag_id
		WHERE feature_flag_associations.object_id = $1
			AND feature_flag_associations.object_type = $2
			AND feature_flags.deleted_at IS NULL
			AND feature_flag_associations.deleted_at IS NULL;
	`

	rows, err := db.Query(ctx, query, objectID, objectType)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list associated flag IDs")
	}

	var ids []int
	err = database.ScanRows(rows, func(row database.Row) error {
		var id int
		if err := row.Scan(&id); err != nil {
			return errors.Wrap(err, "unable to scan flag ID")
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to scan associated flag IDs")
	}

	return ids, nil
}

// SetAssociation creates or removes a single flag association for an object.
// If enabled is true, inserts a new association (or restores a soft-deleted one).
// If enabled is false, soft-deletes the association.
func SetAssociation(
	ctx context.Context,
	db database.Database,
	objectType enums.FeatureFlagObjectType,
	objectID int,
	flagID int,
	enabled bool,
) error {
	user := users.FromContext(ctx)
	var createdBy *int
	if user != nil {
		createdBy = &user.ID
	}

	if enabled {
		// Try to restore soft-deleted association first
		restoreQuery := `
			UPDATE feature_flag_associations
			SET deleted_at = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE feature_flag_id = $1 AND object_id = $2 AND object_type = $3 AND deleted_at IS NOT NULL;
		`
		result, err := db.Exec(ctx, restoreQuery, flagID, objectID, objectType)
		if err != nil {
			return errors.Wrap(err, "unable to restore feature flag association")
		}
		if result.RowsAffected() > 0 {
			return nil
		}
		// Check if active association already exists (idempotent)
		var exists int
		checkQuery := `
			SELECT 1 FROM feature_flag_associations
			WHERE feature_flag_id = $1 AND object_id = $2 AND object_type = $3 AND deleted_at IS NULL;
		`
		err = db.QueryRow(ctx, checkQuery, flagID, objectID, objectType).Scan(&exists)
		if err == nil {
			return nil // already associated
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return errors.Wrap(err, "unable to check feature flag association")
		}
		// No existing row; insert new association
		insertQuery := `
			INSERT INTO feature_flag_associations(feature_flag_id, object_id, object_type, created_by)
			VALUES ($1, $2, $3, $4);
		`
		_, err = db.Exec(ctx, insertQuery, flagID, objectID, objectType, createdBy)
		if err != nil {
			return errors.Wrap(err, "unable to create feature flag association")
		}
		return nil
	}

	// enabled is false: soft-delete the association
	deleteQuery := `
		UPDATE feature_flag_associations
		SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE feature_flag_id = $1 AND object_id = $2 AND object_type = $3 AND deleted_at IS NULL;
	`
	_, err := db.Exec(ctx, deleteQuery, flagID, objectID, objectType)
	if err != nil {
		return errors.Wrap(err, "unable to delete feature flag association")
	}
	return nil
}
