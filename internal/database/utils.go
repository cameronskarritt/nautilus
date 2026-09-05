package database

import (
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"

	"nautilus/internal/errors"
)

// IsUniqueViolation returns true if the error is a unique constraint violation.
func IsUniqueViolation(err error) bool {
	// PostgreSQL: error code 23505
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	// SQLite: SQLITE_CONSTRAINT_UNIQUE (2067) or SQLITE_CONSTRAINT (19)
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code()
		return code == 2067 || code == 19
	}

	return false
}

// IsUniqueViolationOn reports whether err is a PostgreSQL violation of the
// named unique constraint.
func IsUniqueViolationOn(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraint
}
