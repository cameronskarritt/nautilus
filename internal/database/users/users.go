package users

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"uuid"

	"nautilus/internal/crypto/argon2"
	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

func EmailExists(ctx context.Context, db database.Database, email string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM users WHERE LOWER(email) = LOWER($1);`

	var exists bool
	err := db.QueryRow(ctx, query, email).Scan(&exists)
	if err != nil {
		return false, errors.Wrap(err, "unable to query for email existence")
	}

	return exists, nil
}

func UsernameExists(ctx context.Context, db database.Database, username string) (bool, error) {
	query := `SELECT COUNT(*) > 0 FROM users WHERE LOWER(username) = LOWER($1);`

	var exists bool
	err := db.QueryRow(ctx, query, username).Scan(&exists)
	if err != nil {
		return false, errors.Wrap(err, "unable to query for username existence")
	}

	return exists, nil
}

func Register(
	ctx context.Context,
	db database.Database,
	username optional.Optional[string],
	email optional.Optional[string],
	password string,
) (*User, error) {
	hash, err := argon2.GenerateHash(password)
	if err != nil {
		return nil, err
	}

	var id int
	query := `INSERT INTO users(username, email, password_hash) VALUES ($1, $2, $3) RETURNING id;`

	err = db.QueryRow(ctx, query, username, email, hash).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create user")
	}

	user, err := Get(ctx, db, id)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func RegisterWithAuthProvider(
	ctx context.Context,
	db database.Database,
	username optional.Optional[string],
	email optional.Optional[string],
	provider enums.AuthProvider,
	authToken string,
) (*User, error) {
	// Note(CLS): We assume the email is verified here, since we've validated it against the IDP
	query := `INSERT INTO users(username, email, auth_provider, auth_token, verified) VALUES ($1, $2, $3, $4, true) RETURNING id;`

	var id int
	err := db.QueryRow(ctx, query, username, email, provider, authToken).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create user")
	}

	return Get(ctx, db, id)
}

func GetPassword(ctx context.Context, db database.Database, email string) (int, string, bool, error) {
	var id int
	var hash string
	var mfaEnabled bool

	query := `SELECT id, password_hash, mfa_enabled FROM users WHERE LOWER(email) = LOWER($1) AND deleted_at IS NULL AND auth_provider = 'local';`
	err := db.QueryRow(ctx, query, email).Scan(&id, &hash, &mfaEnabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, "", false, nil
		}
		return -1, "", false, errors.Wrap(err, "unable to get password")
	}

	return id, hash, mfaEnabled, nil
}

func Get(ctx context.Context, db database.Database, id int) (*User, error) {
	user := new(User)

	query := `
		SELECT
			id, external_id, email, username, auth_provider, verified, admin, mfa_enabled, created_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL;
	`

	row := db.QueryRow(ctx, query, id)
	err := row.Scan(
		&user.ID,
		&user.ExternalID,
		&user.Email,
		&user.Username,
		&user.AuthProvider,
		&user.Verified,
		&user.Admin,
		&user.MFAEnabled,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch user")
	}

	return user, nil
}

func GetExternal(ctx context.Context, db database.Database, externalID string) (*UserExternal, error) {
	user := new(UserExternal)

	query := `
		SELECT
			id, external_id, username, created_at
		FROM users
		WHERE external_id = $1 AND deleted_at IS NULL;
	`

	row := db.QueryRow(ctx, query, externalID)
	err := row.Scan(
		&user.ID,
		&user.ExternalID,
		&user.Username,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch user by id")
	}

	return user, nil
}

func GetByExternalID(ctx context.Context, db database.Database, externalID string) (*User, error) {
	query := `SELECT id FROM users WHERE external_id = $1 AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, externalID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch user by external id")
	}

	return Get(ctx, db, id)
}

func GetExternalUsername(ctx context.Context, db database.Database, username string) (*UserExternal, error) {
	user := new(UserExternal)

	query := `
		SELECT
			id, external_id, username, created_at
		FROM users
		WHERE username = $1 AND deleted_at IS NULL;
	`

	row := db.QueryRow(ctx, query, username)
	err := row.Scan(
		&user.ID,
		&user.ExternalID,
		&user.Username,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch user by username")
	}

	return user, nil
}

func GetAuthProvider(ctx context.Context, db database.Database, email string) (int, enums.AuthProvider, error) {
	query := `SELECT id, auth_provider FROM users WHERE LOWER(email) = LOWER($1);`

	var id int
	var provider enums.AuthProvider
	err := db.QueryRow(ctx, query, email).Scan(&id, &provider)
	if err != nil {
		return -1, "", errors.Wrap(err, "unable to fetch auth provider")
	}

	return id, provider, nil
}

func GetByAuthProvider(ctx context.Context, db database.Database, provider enums.AuthProvider, authToken string) (*User, error) {
	query := `SELECT id FROM users WHERE auth_provider = $1 AND auth_token = $2;`

	var id int
	err := db.QueryRow(ctx, query, provider, authToken).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch user by auth provider")
	}

	return Get(ctx, db, id)
}

func GetByEmail(ctx context.Context, db database.Database, email string) (*User, error) {
	query := `SELECT id FROM users WHERE LOWER(email) = LOWER($1) AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, email).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch user by email")
	}

	return Get(ctx, db, id)
}

func UpdatePassword(ctx context.Context, db database.Database, userID int, password string) error {
	hash, err := argon2.GenerateHash(password)
	if err != nil {
		return err
	}

	query := `UPDATE users SET password_hash = $1 WHERE id = $2;`

	_, err = db.Exec(ctx, query, hash, userID)
	if err != nil {
		return errors.Wrap(err, "unable to update password")
	}

	return nil
}

func SetVerification(ctx context.Context, db database.Database, userID int, status bool) error {
	query := `UPDATE users SET verified = $1 WHERE id = $2;`

	_, err := db.Exec(ctx, query, status, userID)
	if err != nil {
		return errors.Wrap(err, "unable to set verified flag")
	}

	return nil
}

func UpdateEmail(ctx context.Context, db database.Database, userID int, email string) error {
	query := `UPDATE users SET email = $1 WHERE id = $2;`

	_, err := db.Exec(ctx, query, email, userID)
	if err != nil {
		return errors.Wrap(err, "unable to update email")
	}

	return nil
}

func UpdateUsername(ctx context.Context, db database.Database, userID int, username string) error {
	query := `UPDATE users SET username = $1 WHERE id = $2;`

	_, err := db.Exec(ctx, query, username, userID)
	if err != nil {
		return errors.Wrap(err, "unable to update username")
	}

	return nil
}

func SetAdmin(ctx context.Context, db database.Database, userID int, admin bool) error {
	query := `UPDATE users SET admin = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2;`

	_, err := db.Exec(ctx, query, admin, userID)
	if err != nil {
		return errors.Wrap(err, "unable to set admin status")
	}

	return nil
}

// RegistrationResult contains all entities created during user registration
type RegistrationResult struct {
	User         *User
	Organization *organizations.Organization
	Member       *organizations.Member
}

// RegisterWithPersonalOrg creates a new user along with their personal organization
// and org membership in a single transaction
func RegisterWithPersonalOrg(
	ctx context.Context,
	db database.Database,
	username optional.Optional[string],
	email optional.Optional[string],
	password string,
) (*RegistrationResult, error) {
	var result RegistrationResult

	err := database.Transact(ctx, db, func(txn database.Database) error {
		// Create the user
		user, err := Register(ctx, txn, username, email, password)
		if err != nil {
			return err
		}
		result.User = user

		// Generate a unique slug for the personal org
		slug := generatePersonalOrgSlug(user.Username.Data)

		// Use email as default org name, fallback to username if email not set
		orgName := user.Email.Data
		if !user.Email.Set {
			orgName = user.Username.Data
		}

		// Create personal organization
		org, err := organizations.Create(
			ctx,
			txn,
			slug,
			orgName,
			true, // personal = true
			optional.Optional[organizations.Settings]{},
		)
		if err != nil {
			return err
		}
		result.Organization = org

		// Create org membership with owner role
		member, err := organizations.CreateMember(
			ctx,
			txn,
			user.ID,
			org.ID,
			organizations.RoleOwner,
			optional.Optional[string]{},
		)
		if err != nil {
			return err
		}
		result.Member = member

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// RegisterWithAuthProviderAndPersonalOrg creates a new OAuth user along with their
// personal organization and org membership in a single transaction
func RegisterWithAuthProviderAndPersonalOrg(
	ctx context.Context,
	db database.Database,
	username optional.Optional[string],
	email optional.Optional[string],
	provider enums.AuthProvider,
	authToken string,
) (*RegistrationResult, error) {
	var result RegistrationResult

	err := database.Transact(ctx, db, func(txn database.Database) error {
		// Create the user
		user, err := RegisterWithAuthProvider(ctx, txn, username, email, provider, authToken)
		if err != nil {
			return err
		}
		result.User = user

		// Generate a unique slug for the personal org
		slug := generatePersonalOrgSlug(user.Username.Data)

		// Use email as default org name, fallback to username if email not set
		orgName := user.Email.Data
		if !user.Email.Set {
			orgName = user.Username.Data
		}

		// Create personal organization
		org, err := organizations.Create(
			ctx,
			txn,
			slug,
			orgName,
			true, // personal = true
			optional.Optional[organizations.Settings]{},
		)
		if err != nil {
			return err
		}
		result.Organization = org

		// Create org membership with owner role
		member, err := organizations.CreateMember(
			ctx,
			txn,
			user.ID,
			org.ID,
			organizations.RoleOwner,
			optional.Optional[string]{},
		)
		if err != nil {
			return err
		}
		result.Member = member

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// generatePersonalOrgSlug creates a unique slug for a personal organization
// Format: username-shortid (e.g., "johndoe-a1b2c3")
func generatePersonalOrgSlug(username string) string {
	// Normalize username for slug
	slug := strings.ToLower(username)
	// Add a short random suffix for uniqueness
	suffix := uuid.New().String()[:8]
	return fmt.Sprintf("%s-%s", slug, suffix)
}

// GetByIDs returns a map of user ID to User for the given IDs.
func GetByIDs(ctx context.Context, db database.Database, ids []int) (map[int]*User, error) {
	if len(ids) == 0 {
		return make(map[int]*User), nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, external_id, email, username, auth_provider, verified, admin, mfa_enabled, created_at
		FROM users
		WHERE id IN (%s) AND deleted_at IS NULL;
	`, strings.Join(placeholders, ", "))

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch users by ids")
	}

	result := make(map[int]*User)
	err = database.ScanRows(rows, func(row database.Row) error {
		user := new(User)
		if err := row.Scan(
			&user.ID,
			&user.ExternalID,
			&user.Email,
			&user.Username,
			&user.AuthProvider,
			&user.Verified,
			&user.Admin,
			&user.MFAEnabled,
			&user.CreatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan user")
		}
		result[user.ID] = user
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}
