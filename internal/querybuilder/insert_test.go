package querybuilder

import (
	"testing"

	"nautilus/internal/optional"
	"nautilus/internal/testutil/require"
)

func TestInsertSetPanics(t *testing.T) {
	t.Parallel()

	t.Run("odd argument count", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: Set requires even number of arguments", r)
		}()
		Insert("users").Set("username") //nolint:staticcheck // intentionally testing panic
	})

	t.Run("non-string key", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: Set keys must be strings", r)
		}()
		Insert("users").Set(42, "john")
	})
}

func TestInsertDoUpdateSetPanics(t *testing.T) {
	t.Parallel()

	t.Run("odd argument count", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: DoUpdateSet requires even number of arguments", r)
		}()
		Insert("users").
			Set("email", "john@example.com").
			OnConflict("email").
			DoUpdateSet("status") //nolint:staticcheck // intentionally testing panic
	})

	t.Run("non-string key", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: DoUpdateSet keys must be strings", r)
		}()
		Insert("users").
			Set("email", "john@example.com").
			OnConflict("email").
			DoUpdateSet(42, "active")
	})
}

func TestInsertBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *InsertBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "set single column",
			Setup: func() *InsertBuilder {
				return Insert("users").Set("username", "john")
			},
			ExpectedQuery: "INSERT INTO users (username) VALUES ($1);",
			ExpectedArgs:  []any{"john"},
		},
		{
			Name: "set multiple columns variadic",
			Setup: func() *InsertBuilder {
				return Insert("users").Set(
					"username", "john",
					"email", "john@example.com",
				)
			},
			ExpectedQuery: "INSERT INTO users (username, email) VALUES ($1, $2);",
			ExpectedArgs:  []any{"john", "john@example.com"},
		},
		{
			Name: "set with expression",
			Setup: func() *InsertBuilder {
				return Insert("users").Set(
					"username", "john",
					"created_at", Expr("CURRENT_TIMESTAMP"),
				)
			},
			ExpectedQuery: "INSERT INTO users (username, created_at) VALUES ($1, CURRENT_TIMESTAMP);",
			ExpectedArgs:  []any{"john"},
		},
		{
			Name: "set with optional set and empty",
			Setup: func() *InsertBuilder {
				return Insert("users").Set(
					"username", "john",
					"bio", optional.Set("hi"),
					"nickname", optional.Empty[string](),
				)
			},
			ExpectedQuery: "INSERT INTO users (username, bio) VALUES ($1, $2);",
			ExpectedArgs:  []any{"john", "hi"},
		},
		{
			Name: "set chained calls",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john").
					Set("email", "john@example.com")
			},
			ExpectedQuery: "INSERT INTO users (username, email) VALUES ($1, $2);",
			ExpectedArgs:  []any{"john", "john@example.com"},
		},
		{
			Name: "columns and values single row",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Columns("username", "email").
					Values("john", "john@example.com")
			},
			ExpectedQuery: "INSERT INTO users (username, email) VALUES ($1, $2);",
			ExpectedArgs:  []any{"john", "john@example.com"},
		},
		{
			Name: "columns and values multi row",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Columns("username", "email").
					Values("john", "john@example.com").
					Values("alice", "alice@example.com")
			},
			ExpectedQuery: "INSERT INTO users (username, email) VALUES ($1, $2), ($3, $4);",
			ExpectedArgs:  []any{"john", "john@example.com", "alice", "alice@example.com"},
		},
		{
			Name: "columns and values with Expr",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Columns("username", "created_at").
					Values("john", Expr("CURRENT_TIMESTAMP"))
			},
			ExpectedQuery: "INSERT INTO users (username, created_at) VALUES ($1, CURRENT_TIMESTAMP);",
			ExpectedArgs:  []any{"john"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			query, args, err := tt.Setup().Build()
			require.NoError(t, err)
			require.Equal(t, tt.ExpectedQuery, query)
			require.Equal(t, tt.ExpectedArgs, args)
		})
	}
}

func TestInsertReturning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *InsertBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "single field",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john").
					Returning("id")
			},
			ExpectedQuery: "INSERT INTO users (username) VALUES ($1) RETURNING id;",
			ExpectedArgs:  []any{"john"},
		},
		{
			Name: "multiple fields",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john").
					Returning("id", "username", "created_at")
			},
			ExpectedQuery: "INSERT INTO users (username) VALUES ($1) RETURNING id, username, created_at;",
			ExpectedArgs:  []any{"john"},
		},
		{
			Name: "returning star",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john").
					Returning("*")
			},
			ExpectedQuery: "INSERT INTO users (username) VALUES ($1) RETURNING *;",
			ExpectedArgs:  []any{"john"},
		},
		{
			Name: "chained returning calls",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john").
					Returning("id").
					Returning("created_at")
			},
			ExpectedQuery: "INSERT INTO users (username) VALUES ($1) RETURNING id, created_at;",
			ExpectedArgs:  []any{"john"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			query, args, err := tt.Setup().Build()
			require.NoError(t, err)
			require.Equal(t, tt.ExpectedQuery, query)
			require.Equal(t, tt.ExpectedArgs, args)
		})
	}
}

func TestInsertOnConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *InsertBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "do nothing",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoNothing().
					Returning("id")
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO NOTHING RETURNING id;",
			ExpectedArgs:  []any{"john@example.com"},
		},
		{
			Name: "do update set with excluded (chained)",
			Setup: func() *InsertBuilder {
				return Insert("push_subscriptions").
					Set(
						"user_id", 1,
						"endpoint", "https://example.com/push",
						"key_auth", "auth",
						"key_p256dh", "p256dh",
					).
					OnConflict("user_id", "endpoint").
					DoUpdateSet("key_auth", Excluded("key_auth")).
					DoUpdateSet("key_p256dh", Excluded("key_p256dh")).
					Returning("id")
			},
			ExpectedQuery: "INSERT INTO push_subscriptions (user_id, endpoint, key_auth, key_p256dh) VALUES ($1, $2, $3, $4) ON CONFLICT (user_id, endpoint) DO UPDATE SET key_auth = EXCLUDED.key_auth, key_p256dh = EXCLUDED.key_p256dh RETURNING id;",
			ExpectedArgs:  []any{1, "https://example.com/push", "auth", "p256dh"},
		},
		{
			Name: "do update set variadic pairs in one call",
			Setup: func() *InsertBuilder {
				return Insert("push_subscriptions").
					Set(
						"user_id", 1,
						"endpoint", "https://example.com/push",
						"key_auth", "auth",
						"key_p256dh", "p256dh",
					).
					OnConflict("user_id", "endpoint").
					DoUpdateSet(
						"key_auth", Excluded("key_auth"),
						"key_p256dh", Excluded("key_p256dh"),
					).
					Returning("id")
			},
			ExpectedQuery: "INSERT INTO push_subscriptions (user_id, endpoint, key_auth, key_p256dh) VALUES ($1, $2, $3, $4) ON CONFLICT (user_id, endpoint) DO UPDATE SET key_auth = EXCLUDED.key_auth, key_p256dh = EXCLUDED.key_p256dh RETURNING id;",
			ExpectedArgs:  []any{1, "https://example.com/push", "auth", "p256dh"},
		},
		{
			Name: "do update set variadic with mixed values preserves order",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoUpdateSet(
						"username", Excluded("username"),
						"name", optional.Empty[string](),
						"status", "active",
						"updated_at", Expr("CURRENT_TIMESTAMP"),
					)
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET username = EXCLUDED.username, status = $2, updated_at = CURRENT_TIMESTAMP;",
			ExpectedArgs:  []any{"john@example.com", "active"},
		},
		{
			Name: "do update set with literal value",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoUpdateSet("status", "active")
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET status = $2;",
			ExpectedArgs:  []any{"john@example.com", "active"},
		},
		{
			Name: "do update set with Expr",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoUpdateSet("updated_at", Expr("CURRENT_TIMESTAMP"))
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET updated_at = CURRENT_TIMESTAMP;",
			ExpectedArgs:  []any{"john@example.com"},
		},
		{
			Name: "do update set with optional set unwraps",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoUpdateSet("status", optional.Set("active"))
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET status = $2;",
			ExpectedArgs:  []any{"john@example.com", "active"},
		},
		{
			Name: "do update set with optional empty is skipped",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoUpdateSet("status", "active").
					DoUpdateSet("name", optional.Empty[string]())
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET status = $2;",
			ExpectedArgs:  []any{"john@example.com", "active"},
		},
		{
			Name: "do update set with mixed Expr and value uses correct placeholder numbering",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john", "email", "john@example.com").
					OnConflict("email").
					DoUpdateSet("username", Excluded("username")).
					DoUpdateSet("updated_at", Expr("CURRENT_TIMESTAMP")).
					DoUpdateSet("status", "active")
			},
			ExpectedQuery: "INSERT INTO users (username, email) VALUES ($1, $2) ON CONFLICT (email) DO UPDATE SET username = EXCLUDED.username, updated_at = CURRENT_TIMESTAMP, status = $3;",
			ExpectedArgs:  []any{"john", "john@example.com", "active"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			query, args, err := tt.Setup().Build()
			require.NoError(t, err)
			require.Equal(t, tt.ExpectedQuery, query)
			require.Equal(t, tt.ExpectedArgs, args)
		})
	}
}

func TestInsertBuildErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *InsertBuilder
		ExpectedError string
	}{
		{
			Name: "empty builder",
			Setup: func() *InsertBuilder {
				return Insert("users")
			},
			ExpectedError: "no values to insert",
		},
		{
			Name: "set with only unset optionals",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("bio", optional.Empty[string](), "nickname", optional.Empty[string]())
			},
			ExpectedError: "no values to insert",
		},
		{
			Name: "mixing Set and Columns/Values",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("username", "john").
					Columns("email").
					Values("john@example.com")
			},
			ExpectedError: "cannot mix Set with Columns/Values",
		},
		{
			Name: "Columns without Values",
			Setup: func() *InsertBuilder {
				return Insert("users").Columns("username", "email")
			},
			ExpectedError: "no values for columns",
		},
		{
			Name: "Values without Columns",
			Setup: func() *InsertBuilder {
				return Insert("users").Values("john", "john@example.com")
			},
			ExpectedError: "no columns for values",
		},
		{
			Name: "column/value count mismatch",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Columns("username", "email").
					Values("john")
			},
			ExpectedError: "column/value count mismatch",
		},
		{
			Name: "mismatch on second row",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Columns("username", "email").
					Values("john", "john@example.com").
					Values("alice")
			},
			ExpectedError: "column/value count mismatch",
		},
		{
			Name: "OnConflict without action",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email")
			},
			ExpectedError: "on conflict requires DoNothing or DoUpdateSet",
		},
		{
			Name: "OnConflict with DoNothing and DoUpdateSet",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "john@example.com").
					OnConflict("email").
					DoNothing().
					DoUpdateSet("status", "active")
			},
			ExpectedError: "cannot combine DoNothing with DoUpdateSet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			_, _, err := tt.Setup().Build()
			require.Error(t, err)
			require.Equal(t, tt.ExpectedError, err.Error())
		})
	}
}

func TestInsertDialect(t *testing.T) {
	t.Parallel()

	t.Run("sqlite dialect", func(t *testing.T) {
		t.Parallel()
		query, args, err := Insert("users").
			Dialect(DialectSQLite).
			Set("username", "john", "email", "john@example.com").
			OnConflict("email").
			DoUpdateSet("username", Excluded("username")).
			Returning("id").
			Build()

		require.NoError(t, err)
		require.Equal(t, "INSERT INTO users (username, email) VALUES (?, ?) ON CONFLICT (email) DO UPDATE SET username = EXCLUDED.username RETURNING id;", query)
		require.Equal(t, []any{"john", "john@example.com"}, args)
	})

	t.Run("postgres dialect is default", func(t *testing.T) {
		t.Parallel()
		query, _, err := Insert("users").Set("username", "john").Build()
		require.NoError(t, err)
		require.Equal(t, "INSERT INTO users (username) VALUES ($1);", query)
	})
}

func TestExcluded(t *testing.T) {
	t.Parallel()

	require.Equal(t, Expr("EXCLUDED.key_auth"), Excluded("key_auth"))
	require.Equal(t, Expr("EXCLUDED.x"), Excluded("x"))
}

func TestInsertSQLInjectionPrevention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *InsertBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "SQL injection in Set value",
			Setup: func() *InsertBuilder {
				return Insert("users").Set("username", "'; DROP TABLE users; --")
			},
			ExpectedQuery: "INSERT INTO users (username) VALUES ($1);",
			ExpectedArgs:  []any{"'; DROP TABLE users; --"},
		},
		{
			Name: "SQL injection in Values",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Columns("username", "email").
					Values("admin' --", "'; DELETE FROM users; --")
			},
			ExpectedQuery: "INSERT INTO users (username, email) VALUES ($1, $2);",
			ExpectedArgs:  []any{"admin' --", "'; DELETE FROM users; --"},
		},
		{
			Name: "SQL injection in DoUpdateSet value",
			Setup: func() *InsertBuilder {
				return Insert("users").
					Set("email", "user@example.com").
					OnConflict("email").
					DoUpdateSet("status", "'; DROP TABLE users; --")
			},
			ExpectedQuery: "INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET status = $2;",
			ExpectedArgs:  []any{"user@example.com", "'; DROP TABLE users; --"},
		},
		{
			Name: "Bobby Tables attack",
			Setup: func() *InsertBuilder {
				return Insert("students").Set("name", "Robert'); DROP TABLE students;--")
			},
			ExpectedQuery: "INSERT INTO students (name) VALUES ($1);",
			ExpectedArgs:  []any{"Robert'); DROP TABLE students;--"},
		},
		{
			Name: "unicode injection attempt",
			Setup: func() *InsertBuilder {
				return Insert("users").Set("bio", "' OR '1'='1' -- 注入攻击")
			},
			ExpectedQuery: "INSERT INTO users (bio) VALUES ($1);",
			ExpectedArgs:  []any{"' OR '1'='1' -- 注入攻击"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			query, args, err := tt.Setup().Build()
			require.NoError(t, err)
			require.Equal(t, tt.ExpectedQuery, query)
			require.Equal(t, tt.ExpectedArgs, args)

			for _, arg := range tt.ExpectedArgs {
				if str, ok := arg.(string); ok && str != "" {
					require.NotContains(t, query, str, "malicious string should not appear in SQL query")
				}
			}
		})
	}
}
