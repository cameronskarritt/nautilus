package querybuilder

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestDeleteBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *DeleteBuilder
		ExpectedQuery string
		ExpectedArgs  []any
		ExpectedError bool
	}{
		{
			Name: "simple delete",
			Setup: func() *DeleteBuilder {
				return Delete("users").Where(Eq{"id": 1})
			},
			ExpectedQuery: "DELETE FROM users WHERE id = $1;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "delete with null where",
			Setup: func() *DeleteBuilder {
				return Delete("users").Where(Eq{"id": 1, "deleted_at": nil})
			},
			ExpectedQuery: "DELETE FROM users WHERE deleted_at IS NULL AND id = $1;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "delete with multiple ANDed conditions",
			Setup: func() *DeleteBuilder {
				return Delete("push_subscriptions").
					Where(Eq{"user_id": 7, "endpoint": "https://example.com/x"})
			},
			ExpectedQuery: "DELETE FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2;",
			ExpectedArgs:  []any{"https://example.com/x", 7},
		},
		{
			Name: "delete with comparison operators",
			Setup: func() *DeleteBuilder {
				return Delete("sessions").
					Where(Eq{"user_id": 1}, Lt("expires_at", "2024-01-01"))
			},
			ExpectedQuery: "DELETE FROM sessions WHERE user_id = $1 AND expires_at < $2;",
			ExpectedArgs:  []any{1, "2024-01-01"},
		},
		{
			Name: "delete with IN clause",
			Setup: func() *DeleteBuilder {
				return Delete("users").Where(Eq{"id": In(1, 2, 3)})
			},
			ExpectedQuery: "DELETE FROM users WHERE id IN ($1, $2, $3);",
			ExpectedArgs:  []any{1, 2, 3},
		},
		{
			Name: "delete with Or group",
			Setup: func() *DeleteBuilder {
				return Delete("users").
					Where(Or{Eq{"status": "banned"}, Eq{"status": "deleted"}})
			},
			ExpectedQuery: "DELETE FROM users WHERE (status = $1 OR status = $2);",
			ExpectedArgs:  []any{"banned", "deleted"},
		},
		{
			Name: "delete with RETURNING single field",
			Setup: func() *DeleteBuilder {
				return Delete("users").Where(Eq{"id": 1}).Returning("id")
			},
			ExpectedQuery: "DELETE FROM users WHERE id = $1 RETURNING id;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "delete with RETURNING multiple fields",
			Setup: func() *DeleteBuilder {
				return Delete("users").
					Where(Eq{"id": 1}).
					Returning("id", "username", "deleted_at")
			},
			ExpectedQuery: "DELETE FROM users WHERE id = $1 RETURNING id, username, deleted_at;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "delete with RETURNING all",
			Setup: func() *DeleteBuilder {
				return Delete("users").Where(Eq{"id": 1}).Returning("*")
			},
			ExpectedQuery: "DELETE FROM users WHERE id = $1 RETURNING *;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "delete with chained RETURNING",
			Setup: func() *DeleteBuilder {
				return Delete("users").
					Where(Eq{"id": 1}).
					Returning("id").
					Returning("username")
			},
			ExpectedQuery: "DELETE FROM users WHERE id = $1 RETURNING id, username;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "delete with SQLite dialect",
			Setup: func() *DeleteBuilder {
				return Delete("users").
					Dialect(DialectSQLite).
					Where(Eq{"id": 1, "org_id": 2})
			},
			ExpectedQuery: "DELETE FROM users WHERE id = ? AND org_id = ?;",
			ExpectedArgs:  []any{1, 2},
		},
		{
			Name: "delete without where errors",
			Setup: func() *DeleteBuilder {
				return Delete("users")
			},
			ExpectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			builder := tt.Setup()
			query, args, err := builder.Build()

			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.ExpectedQuery, query)
			require.Equal(t, tt.ExpectedArgs, args)
		})
	}
}

func TestDeleteChaining(t *testing.T) {
	t.Parallel()

	query, args, err := Delete("users").
		Where(Eq{"org_id": 1}).
		Where(Gt("age", 18)).
		Returning("id").
		Build()

	require.NoError(t, err)
	require.Equal(t, "DELETE FROM users WHERE org_id = $1 AND age > $2 RETURNING id;", query)
	require.Equal(t, []any{1, 18}, args)
}

func TestDeleteOr(t *testing.T) {
	t.Parallel()

	t.Run("simple OR", func(t *testing.T) {
		t.Parallel()
		query, args, err := Delete("users").
			Where(Or{Eq{"status": "banned"}, Eq{"status": "deleted"}}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "DELETE FROM users WHERE (status = $1 OR status = $2);", query)
		require.Equal(t, []any{"banned", "deleted"}, args)
	})

	t.Run("OR with AND", func(t *testing.T) {
		t.Parallel()
		query, args, err := Delete("users").
			Where(Eq{"org_id": 1}, Or{Eq{"status": "banned"}, Eq{"status": "deleted"}}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "DELETE FROM users WHERE org_id = $1 AND (status = $2 OR status = $3);", query)
		require.Equal(t, []any{1, "banned", "deleted"}, args)
	})

	t.Run("OR with IS NULL", func(t *testing.T) {
		t.Parallel()
		query, args, err := Delete("users").
			Where(Or{Eq{"verified_at": nil}, Lt("score", 50)}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "DELETE FROM users WHERE (verified_at IS NULL OR score < $1);", query)
		require.Equal(t, []any{50}, args)
	})
}

func TestDeleteSQLInjectionPrevention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *DeleteBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "injection attempt in WHERE value",
			Setup: func() *DeleteBuilder {
				return Delete("users").Where(Eq{"username": "admin' OR '1'='1"})
			},
			ExpectedQuery: "DELETE FROM users WHERE username = $1;",
			ExpectedArgs:  []any{"admin' OR '1'='1"},
		},
		{
			Name: "Bobby Tables in WHERE",
			Setup: func() *DeleteBuilder {
				return Delete("students").
					Where(Eq{"name": "Robert'); DROP TABLE students;--"})
			},
			ExpectedQuery: "DELETE FROM students WHERE name = $1;",
			ExpectedArgs:  []any{"Robert'); DROP TABLE students;--"},
		},
		{
			Name: "injection in IN clause",
			Setup: func() *DeleteBuilder {
				return Delete("users").
					Where(Eq{"status": In("active", "'; DROP TABLE users; --")})
			},
			ExpectedQuery: "DELETE FROM users WHERE status IN ($1, $2);",
			ExpectedArgs:  []any{"active", "'; DROP TABLE users; --"},
		},
		{
			Name: "injection in OR conditions",
			Setup: func() *DeleteBuilder {
				return Delete("users").
					Where(Or{
						Eq{"status": "'; DROP TABLE users; --"},
						Eq{"username": "admin' OR '1'='1"},
					})
			},
			ExpectedQuery: "DELETE FROM users WHERE (status = $1 OR username = $2);",
			ExpectedArgs:  []any{"'; DROP TABLE users; --", "admin' OR '1'='1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			builder := tt.Setup()
			query, args, err := builder.Build()

			require.NoError(t, err)
			require.Equal(t, tt.ExpectedQuery, query)
			require.Equal(t, tt.ExpectedArgs, args)
		})
	}
}
