package querybuilder

import (
	"testing"

	"nautilus/internal/optional"
	"nautilus/internal/testutil/require"
)

func TestSetPanics(t *testing.T) {
	t.Parallel()

	t.Run("odd argument count", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: Set requires even number of arguments", r)
		}()
		Update("users").Set("column") //nolint:staticcheck // intentionally testing panic
	})

	t.Run("non-string key", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: Set keys must be strings", r)
		}()
		Update("users").Set(42, "john")
	})
}

func TestBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *UpdateBuilder
		ExpectedQuery string
		ExpectedArgs  []any
		ExpectedError bool
	}{
		{
			Name: "simple update",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("username", "john").
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET username = $1 WHERE id = $2;",
			ExpectedArgs:  []any{"john", 1},
		},
		{
			Name: "update with expression",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("updated_at", Expr("CURRENT_TIMESTAMP")).
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET updated_at = CURRENT_TIMESTAMP WHERE id = $1;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "update with null where",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("username", "john").
					Where(Eq{"id": 1, "deleted_at": nil})
			},
			ExpectedQuery: "UPDATE users SET username = $1 WHERE deleted_at IS NULL AND id = $2;",
			ExpectedArgs:  []any{"john", 1},
		},
		{
			Name: "update without where",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("status", "active")
			},
			ExpectedQuery: "UPDATE users SET status = $1;",
			ExpectedArgs:  []any{"active"},
		},
		{
			Name: "multiple set clauses",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("username", "john").
					Set("email", "john@example.com").
					Set("updated_at", Expr("CURRENT_TIMESTAMP")).
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET username = $1, email = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3;",
			ExpectedArgs:  []any{"john", "john@example.com", 1},
		},
		{
			Name: "with optional values mixed",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set(
						"username", optional.Set("john"),
						"bio", optional.Empty[string](),
						"updated_at", Expr("CURRENT_TIMESTAMP"),
					).
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET username = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2;",
			ExpectedArgs:  []any{"john", 1},
		},
		{
			Name: "no set clauses",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Where(Eq{"id": 1})
			},
			ExpectedError: true,
		},
		{
			Name: "only unset optionals",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("username", optional.Empty[string]()).
					Where(Eq{"id": 1})
			},
			ExpectedError: true,
		},
		{
			Name: "with comparison operators",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("status", "locked").
					Where(Eq{"org_id": 1}, Gt("login_attempts", 5), Lt("last_login_days", 90))
			},
			ExpectedQuery: "UPDATE users SET status = $1 WHERE org_id = $2 AND login_attempts > $3 AND last_login_days < $4;",
			ExpectedArgs:  []any{"locked", 1, 5, 90},
		},
		{
			Name: "with mixed operators",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("verified", true).
					Where(Eq{"deleted_at": nil, "org_id": 1}, Gte("age", 18))
			},
			ExpectedQuery: "UPDATE users SET verified = $1 WHERE deleted_at IS NULL AND org_id = $2 AND age >= $3;",
			ExpectedArgs:  []any{true, 1, 18},
		},
		{
			Name: "with IN clause from ints",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("status", "active").
					Where(Eq{"id": In(1, 2, 3)})
			},
			ExpectedQuery: "UPDATE users SET status = $1 WHERE id IN ($2, $3, $4);",
			ExpectedArgs:  []any{"active", 1, 2, 3},
		},
		{
			Name: "with IN clause from strings",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("notified", true).
					Where(Eq{"status": In("active", "pending")})
			},
			ExpectedQuery: "UPDATE users SET notified = $1 WHERE status IN ($2, $3);",
			ExpectedArgs:  []any{true, "active", "pending"},
		},
		{
			Name: "with IN clause and regular condition",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("flagged", true).
					Where(Eq{"org_id": 1, "status": In("a", "b")})
			},
			ExpectedQuery: "UPDATE users SET flagged = $1 WHERE org_id = $2 AND status IN ($3, $4);",
			ExpectedArgs:  []any{true, 1, "a", "b"},
		},
		{
			Name: "with single element IN clause",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("checked", true).
					Where(Eq{"id": In(42)})
			},
			ExpectedQuery: "UPDATE users SET checked = $1 WHERE id IN ($2);",
			ExpectedArgs:  []any{true, 42},
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

func TestChaining(t *testing.T) {
	t.Parallel()

	query, args, err := Update("users").
		Set("username", "john").
		Set("bio", optional.Set("Hello world")).
		Set("updated_at", Expr("CURRENT_TIMESTAMP")).
		Where(Eq{"id": 1}).
		Where(Eq{"org_id": 2}).
		Build()

	require.NoError(t, err)
	require.Equal(t, "UPDATE users SET username = $1, bio = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3 AND org_id = $4;", query)
	require.Equal(t, []any{"john", "Hello world", 1, 2}, args)
}

func TestOr(t *testing.T) {
	t.Parallel()

	t.Run("simple OR", func(t *testing.T) {
		t.Parallel()
		query, args, err := Update("users").
			Set("notified", true).
			Where(Or{Eq{"status": "active"}, Eq{"status": "pending"}}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "UPDATE users SET notified = $1 WHERE (status = $2 OR status = $3);", query)
		require.Equal(t, []any{true, "active", "pending"}, args)
	})

	t.Run("OR with AND", func(t *testing.T) {
		t.Parallel()
		query, args, err := Update("users").
			Set("notified", true).
			Where(Eq{"org_id": 1}, Or{Eq{"status": "active"}, Eq{"status": "pending"}}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "UPDATE users SET notified = $1 WHERE org_id = $2 AND (status = $3 OR status = $4);", query)
		require.Equal(t, []any{true, 1, "active", "pending"}, args)
	})

	t.Run("OR with comparison operators", func(t *testing.T) {
		t.Parallel()
		query, args, err := Update("users").
			Set("discount", 10).
			Where(Or{Gt("age", 65), Lt("age", 18)}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "UPDATE users SET discount = $1 WHERE (age > $2 OR age < $3);", query)
		require.Equal(t, []any{10, 65, 18}, args)
	})

	t.Run("OR with IS NULL", func(t *testing.T) {
		t.Parallel()
		query, args, err := Update("users").
			Set("needs_review", true).
			Where(Or{Eq{"verified_at": nil}, Lt("score", 50)}).
			Build()

		require.NoError(t, err)
		require.Equal(t, "UPDATE users SET needs_review = $1 WHERE (verified_at IS NULL OR score < $2);", query)
		require.Equal(t, []any{true, 50}, args)
	})

	t.Run("multiple ORs with AND", func(t *testing.T) {
		t.Parallel()
		query, args, err := Update("users").
			Set("flagged", true).
			Where(
				Eq{"org_id": 1},
				Or{Eq{"status": "active"}, Eq{"status": "pending"}},
				Or{Gt("age", 65), Lt("age", 18)},
			).
			Build()

		require.NoError(t, err)
		require.Equal(t, "UPDATE users SET flagged = $1 WHERE org_id = $2 AND (status = $3 OR status = $4) AND (age > $5 OR age < $6);", query)
		require.Equal(t, []any{true, 1, "active", "pending", 65, 18}, args)
	})
}

func TestReturning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *UpdateBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "single field",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("name", "John").
					Where(Eq{"id": 1}).
					Returning("id")
			},
			ExpectedQuery: "UPDATE users SET name = $1 WHERE id = $2 RETURNING id;",
			ExpectedArgs:  []any{"John", 1},
		},
		{
			Name: "multiple fields",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("name", "John").
					Where(Eq{"id": 1}).
					Returning("id", "name", "updated_at")
			},
			ExpectedQuery: "UPDATE users SET name = $1 WHERE id = $2 RETURNING id, name, updated_at;",
			ExpectedArgs:  []any{"John", 1},
		},
		{
			Name: "chained returning calls",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("name", "John").
					Where(Eq{"id": 1}).
					Returning("id").
					Returning("name")
			},
			ExpectedQuery: "UPDATE users SET name = $1 WHERE id = $2 RETURNING id, name;",
			ExpectedArgs:  []any{"John", 1},
		},
		{
			Name: "returning all",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("status", "active").
					Where(Eq{"id": 1}).
					Returning("*")
			},
			ExpectedQuery: "UPDATE users SET status = $1 WHERE id = $2 RETURNING *;",
			ExpectedArgs:  []any{"active", 1},
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

func TestSQLInjectionPrevention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *UpdateBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "SQL injection attempt in SET value",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("username", "'; DROP TABLE users; --").
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET username = $1 WHERE id = $2;",
			ExpectedArgs:  []any{"'; DROP TABLE users; --", 1},
		},
		{
			Name: "SQL injection attempt in WHERE value",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("status", "active").
					Where(Eq{"username": "admin' OR '1'='1"})
			},
			ExpectedQuery: "UPDATE users SET status = $1 WHERE username = $2;",
			ExpectedArgs:  []any{"active", "admin' OR '1'='1"},
		},
		{
			Name: "Bobby Tables attack",
			Setup: func() *UpdateBuilder {
				return Update("students").
					Set("name", "Robert'); DROP TABLE students;--").
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE students SET name = $1 WHERE id = $2;",
			ExpectedArgs:  []any{"Robert'); DROP TABLE students;--", 1},
		},
		{
			Name: "DELETE injection attempt",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("bio", "'; DELETE FROM users WHERE '1'='1").
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET bio = $1 WHERE id = $2;",
			ExpectedArgs:  []any{"'; DELETE FROM users WHERE '1'='1", 1},
		},
		{
			Name: "OR 1=1 injection in WHERE",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("verified", true).
					Where(Eq{"id": "1 OR 1=1"})
			},
			ExpectedQuery: "UPDATE users SET verified = $1 WHERE id = $2;",
			ExpectedArgs:  []any{true, "1 OR 1=1"},
		},
		{
			Name: "multiple injection attempts",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("email", "'; DROP TABLE users; --").
					Set("username", "admin' --").
					Where(Eq{"id": "1' OR '1'='1", "org_id": "2; DELETE FROM orgs; --"})
			},
			ExpectedQuery: "UPDATE users SET email = $1, username = $2 WHERE id = $3 AND org_id = $4;",
			ExpectedArgs:  []any{"'; DROP TABLE users; --", "admin' --", "1' OR '1'='1", "2; DELETE FROM orgs; --"},
		},
		{
			Name: "injection with comparison operators",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("status", "'; DROP TABLE users; --").
					Where(Gt("age", "18' OR '1'='1"), Lt("score", "100; DELETE FROM users; --"))
			},
			ExpectedQuery: "UPDATE users SET status = $1 WHERE age > $2 AND score < $3;",
			ExpectedArgs:  []any{"'; DROP TABLE users; --", "18' OR '1'='1", "100; DELETE FROM users; --"},
		},
		{
			Name: "injection in IN clause",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("flagged", true).
					Where(Eq{"status": In("active", "'; DROP TABLE users; --", "pending' OR '1'='1")})
			},
			ExpectedQuery: "UPDATE users SET flagged = $1 WHERE status IN ($2, $3, $4);",
			ExpectedArgs:  []any{true, "active", "'; DROP TABLE users; --", "pending' OR '1'='1"},
		},
		{
			Name: "injection in OR conditions",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("reviewed", true).
					Where(Or{Eq{"status": "'; DROP TABLE users; --"}, Eq{"username": "admin' OR '1'='1"}})
			},
			ExpectedQuery: "UPDATE users SET reviewed = $1 WHERE (status = $2 OR username = $3);",
			ExpectedArgs:  []any{true, "'; DROP TABLE users; --", "admin' OR '1'='1"},
		},
		{
			Name: "unicode and special characters",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("bio", "' OR '1'='1' -- 注入攻击").
					Where(Eq{"username": "user\u0027 OR \u00271\u0027=\u00271"})
			},
			ExpectedQuery: "UPDATE users SET bio = $1 WHERE username = $2;",
			ExpectedArgs:  []any{"' OR '1'='1' -- 注入攻击", "user' OR '1'='1"},
		},
		{
			Name: "nested quotes and semicolons",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("description", "Test'; EXEC sp_executesql N'DROP TABLE users'; --").
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET description = $1 WHERE id = $2;",
			ExpectedArgs:  []any{"Test'; EXEC sp_executesql N'DROP TABLE users'; --", 1},
		},
		{
			Name: "injection with UNION attack",
			Setup: func() *UpdateBuilder {
				return Update("users").
					Set("email", "user@example.com' UNION SELECT password FROM admin_users --").
					Where(Eq{"id": 1})
			},
			ExpectedQuery: "UPDATE users SET email = $1 WHERE id = $2;",
			ExpectedArgs:  []any{"user@example.com' UNION SELECT password FROM admin_users --", 1},
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

			// Verify the query structure is intact
			require.Len(t, args, len(tt.ExpectedArgs))

			// Verify no malicious strings leaked into the SQL query structure
			// Only placeholders like $1, $2, etc. should appear
			for _, arg := range tt.ExpectedArgs {
				if str, ok := arg.(string); ok && str != "" {
					require.NotContains(t, query, str, "malicious string should not appear in SQL query")
				}
			}
		})
	}
}
