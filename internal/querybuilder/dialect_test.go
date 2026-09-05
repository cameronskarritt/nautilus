package querybuilder

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestDialectPostgres(t *testing.T) {
	t.Parallel()

	require.Equal(t, "$1", DialectPostgres.Placeholder(1))
	require.Equal(t, "$5", DialectPostgres.Placeholder(5))
	require.Equal(t, "$100", DialectPostgres.Placeholder(100))
}

func TestDialectSQLite(t *testing.T) {
	t.Parallel()

	require.Equal(t, "?", DialectSQLite.Placeholder(1))
	require.Equal(t, "?", DialectSQLite.Placeholder(5))
	require.Equal(t, "?", DialectSQLite.Placeholder(100))
}

func TestSQLiteDialect(t *testing.T) {
	t.Parallel()

	query, args, err := Update("users").
		Dialect(DialectSQLite).
		Set("username", "john").
		Set("email", "john@example.com").
		Where(Eq{"id": 1, "deleted_at": nil}).
		Build()

	require.NoError(t, err)
	require.Equal(t, "UPDATE users SET username = ?, email = ? WHERE deleted_at IS NULL AND id = ?;", query)
	require.Equal(t, []any{"john", "john@example.com", 1}, args)
}
