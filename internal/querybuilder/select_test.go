package querybuilder

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestSelectBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *SelectBuilder
		ExpectedQuery string
		ExpectedArgs  []any
		ExpectedError bool
	}{
		{
			Name: "simple select",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users")
			},
			ExpectedQuery: "SELECT id, name FROM users;",
			ExpectedArgs:  nil,
		},
		{
			Name: "select all",
			Setup: func() *SelectBuilder {
				return Select("*").From("users")
			},
			ExpectedQuery: "SELECT * FROM users;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with where",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").Where(Eq{"id": 1})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE id = $1;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "with multiple where conditions",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").Where(Eq{"org_id": 1, "deleted_at": nil})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE deleted_at IS NULL AND org_id = $1;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "with distinct",
			Setup: func() *SelectBuilder {
				return Select("status").Distinct().From("users")
			},
			ExpectedQuery: "SELECT DISTINCT status FROM users;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with order by asc",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").OrderBy(Asc, "name")
			},
			ExpectedQuery: "SELECT id, name FROM users ORDER BY name ASC;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with order by desc",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").OrderBy(Desc, "created_at")
			},
			ExpectedQuery: "SELECT id, name FROM users ORDER BY created_at DESC;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with multiple order by chained",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					OrderBy(Asc, "name").
					OrderBy(Desc, "created_at")
			},
			ExpectedQuery: "SELECT id, name FROM users ORDER BY name ASC, created_at DESC;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with multiple order by pairs in one call",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					OrderBy(Asc, "name", Desc, "created_at")
			},
			ExpectedQuery: "SELECT id, name FROM users ORDER BY name ASC, created_at DESC;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with limit",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").Limit(10)
			},
			ExpectedQuery: "SELECT id, name FROM users LIMIT 10;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with offset",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").Offset(20)
			},
			ExpectedQuery: "SELECT id, name FROM users OFFSET 20;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with limit and offset",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").Limit(10).Offset(20)
			},
			ExpectedQuery: "SELECT id, name FROM users LIMIT 10 OFFSET 20;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with group by",
			Setup: func() *SelectBuilder {
				return Select("status", "COUNT(*)").From("users").GroupBy("status")
			},
			ExpectedQuery: "SELECT status, COUNT(*) FROM users GROUP BY status;",
			ExpectedArgs:  nil,
		},
		{
			Name: "with group by and having",
			Setup: func() *SelectBuilder {
				return Select("status", "COUNT(*)").From("users").
					GroupBy("status").
					Having(Gt("COUNT(*)", 5))
			},
			ExpectedQuery: "SELECT status, COUNT(*) FROM users GROUP BY status HAVING COUNT(*) > $1;",
			ExpectedArgs:  []any{5},
		},
		{
			Name: "with suffix for update",
			Setup: func() *SelectBuilder {
				return Select("id", "balance").From("accounts").
					Where(Eq{"id": 1}).
					Suffix("FOR UPDATE")
			},
			ExpectedQuery: "SELECT id, balance FROM accounts WHERE id = $1 FOR UPDATE;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "with suffix for share",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					Where(Eq{"id": 1}).
					Suffix("FOR SHARE NOWAIT")
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE id = $1 FOR SHARE NOWAIT;",
			ExpectedArgs:  []any{1},
		},
		{
			Name: "full featured query",
			Setup: func() *SelectBuilder {
				return Select("status", "COUNT(*)").
					Distinct().
					From("users").
					Where(Eq{"deleted_at": nil}).
					GroupBy("status").
					Having(Gt("COUNT(*)", 5)).
					OrderBy(Desc, "COUNT(*)").
					Limit(10).
					Offset(20)
			},
			ExpectedQuery: "SELECT DISTINCT status, COUNT(*) FROM users WHERE deleted_at IS NULL GROUP BY status HAVING COUNT(*) > $1 ORDER BY COUNT(*) DESC LIMIT 10 OFFSET 20;",
			ExpectedArgs:  []any{5},
		},
		{
			Name: "with IN clause",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					Where(Eq{"status": In("active", "pending")})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE status IN ($1, $2);",
			ExpectedArgs:  []any{"active", "pending"},
		},
		{
			Name: "with OR conditions",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					Where(Or{Eq{"status": "active"}, Eq{"status": "pending"}})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE (status = $1 OR status = $2);",
			ExpectedArgs:  []any{"active", "pending"},
		},
		{
			Name: "with comparison operators",
			Setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					Where(Gte("age", 18), Lt("age", 65))
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE age >= $1 AND age < $2;",
			ExpectedArgs:  []any{18, 65},
		},
		{
			Name:          "no fields error",
			Setup:         func() *SelectBuilder { return Select().From("users") },
			ExpectedError: true,
		},
		{
			Name:          "no table error",
			Setup:         func() *SelectBuilder { return Select("id", "name") },
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

func TestOrderByPanics(t *testing.T) {
	t.Parallel()

	t.Run("odd argument count", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: OrderBy requires even number of arguments", r)
		}()
		Select("id").From("users").OrderBy(Asc) //nolint:staticcheck // intentionally testing panic
	})

	t.Run("non-OrderDirection direction", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: OrderBy direction args must be OrderDirection", r)
		}()
		Select("id").From("users").OrderBy("ASC", "name")
	})

	t.Run("non-string column", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			require.Equal(t, "querybuilder: OrderBy column args must be strings", r)
		}()
		Select("id").From("users").OrderBy(Asc, 42)
	})
}

func TestSelectSQLiteDialect(t *testing.T) {
	t.Parallel()

	query, args, err := Select("id", "name").
		Dialect(DialectSQLite).
		From("users").
		Where(Eq{"id": 1, "deleted_at": nil}).
		Build()

	require.NoError(t, err)
	require.Equal(t, "SELECT id, name FROM users WHERE deleted_at IS NULL AND id = ?;", query)
	require.Equal(t, []any{1}, args)
}

func TestSelectChaining(t *testing.T) {
	t.Parallel()

	query, args, err := Select("id", "name", "email").
		Distinct().
		From("users").
		Where(Eq{"org_id": 1}).
		Where(Eq{"deleted_at": nil}).
		GroupBy("department").
		Having(Gt("COUNT(*)", 1)).
		OrderBy(Asc, "name").
		Limit(50).
		Offset(100).
		Suffix("FOR UPDATE").
		Build()

	require.NoError(t, err)
	require.Equal(t, "SELECT DISTINCT id, name, email FROM users WHERE org_id = $1 AND deleted_at IS NULL GROUP BY department HAVING COUNT(*) > $2 ORDER BY name ASC LIMIT 50 OFFSET 100 FOR UPDATE;", query)
	require.Equal(t, []any{1, 1}, args)
}

func TestSelectSQLInjectionPrevention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Setup         func() *SelectBuilder
		ExpectedQuery string
		ExpectedArgs  []any
	}{
		{
			Name: "SQL injection attempt in WHERE value",
			Setup: func() *SelectBuilder {
				return Select("id", "name").
					From("users").
					Where(Eq{"username": "admin' OR '1'='1"})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE username = $1;",
			ExpectedArgs:  []any{"admin' OR '1'='1"},
		},
		{
			Name: "DROP TABLE injection in WHERE",
			Setup: func() *SelectBuilder {
				return Select("*").
					From("users").
					Where(Eq{"email": "'; DROP TABLE users; --"})
			},
			ExpectedQuery: "SELECT * FROM users WHERE email = $1;",
			ExpectedArgs:  []any{"'; DROP TABLE users; --"},
		},
		{
			Name: "Bobby Tables in WHERE",
			Setup: func() *SelectBuilder {
				return Select("id", "name").
					From("students").
					Where(Eq{"name": "Robert'); DROP TABLE students;--"})
			},
			ExpectedQuery: "SELECT id, name FROM students WHERE name = $1;",
			ExpectedArgs:  []any{"Robert'); DROP TABLE students;--"},
		},
		{
			Name: "UNION injection attempt",
			Setup: func() *SelectBuilder {
				return Select("id", "email").
					From("users").
					Where(Eq{"id": "1' UNION SELECT password, username FROM admin_users --"})
			},
			ExpectedQuery: "SELECT id, email FROM users WHERE id = $1;",
			ExpectedArgs:  []any{"1' UNION SELECT password, username FROM admin_users --"},
		},
		{
			Name: "OR 1=1 injection",
			Setup: func() *SelectBuilder {
				return Select("*").
					From("users").
					Where(Eq{"id": "1 OR 1=1", "status": "active' OR 'x'='x"})
			},
			ExpectedQuery: "SELECT * FROM users WHERE id = $1 AND status = $2;",
			ExpectedArgs:  []any{"1 OR 1=1", "active' OR 'x'='x"},
		},
		{
			Name: "injection in comparison operators",
			Setup: func() *SelectBuilder {
				return Select("id", "name").
					From("users").
					Where(Gt("age", "18' OR '1'='1"), Lt("score", "100; DELETE FROM users; --"))
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE age > $1 AND score < $2;",
			ExpectedArgs:  []any{"18' OR '1'='1", "100; DELETE FROM users; --"},
		},
		{
			Name: "injection in LIKE operator",
			Setup: func() *SelectBuilder {
				return Select("id", "email").
					From("users").
					Where(Like("email", "%'; DROP TABLE users; --"))
			},
			ExpectedQuery: "SELECT id, email FROM users WHERE email LIKE $1;",
			ExpectedArgs:  []any{"%'; DROP TABLE users; --"},
		},
		{
			Name: "injection in IN clause",
			Setup: func() *SelectBuilder {
				return Select("id", "name").
					From("users").
					Where(Eq{"status": In("active", "'; DROP TABLE users; --", "pending' OR '1'='1")})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE status IN ($1, $2, $3);",
			ExpectedArgs:  []any{"active", "'; DROP TABLE users; --", "pending' OR '1'='1"},
		},
		{
			Name: "injection in HAVING clause",
			Setup: func() *SelectBuilder {
				return Select("status", "COUNT(*)").
					From("users").
					GroupBy("status").
					Having(Gt("COUNT(*)", "5' OR '1'='1"))
			},
			ExpectedQuery: "SELECT status, COUNT(*) FROM users GROUP BY status HAVING COUNT(*) > $1;",
			ExpectedArgs:  []any{"5' OR '1'='1"},
		},
		{
			Name: "injection in OR conditions",
			Setup: func() *SelectBuilder {
				return Select("id", "name").
					From("users").
					Where(Or{Eq{"status": "'; DROP TABLE users; --"}, Eq{"username": "admin' OR '1'='1"}})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE (status = $1 OR username = $2);",
			ExpectedArgs:  []any{"'; DROP TABLE users; --", "admin' OR '1'='1"},
		},
		{
			Name: "multiple WHERE conditions with injection",
			Setup: func() *SelectBuilder {
				return Select("*").
					From("users").
					Where(Eq{"org_id": "1' OR '1'='1"}).
					Where(Eq{"deleted_at": nil}).
					Where(Ne("status", "'; DELETE FROM users; --"))
			},
			ExpectedQuery: "SELECT * FROM users WHERE org_id = $1 AND deleted_at IS NULL AND status <> $2;",
			ExpectedArgs:  []any{"1' OR '1'='1", "'; DELETE FROM users; --"},
		},
		{
			Name: "injection with stacked queries",
			Setup: func() *SelectBuilder {
				return Select("id", "email").
					From("users").
					Where(Eq{"username": "admin'; DROP TABLE sessions; SELECT * FROM users WHERE '1'='1"})
			},
			ExpectedQuery: "SELECT id, email FROM users WHERE username = $1;",
			ExpectedArgs:  []any{"admin'; DROP TABLE sessions; SELECT * FROM users WHERE '1'='1"},
		},
		{
			Name: "injection with comment variations",
			Setup: func() *SelectBuilder {
				return Select("id").
					From("users").
					Where(Eq{"id": "1' -- ", "username": "admin' /*", "email": "test@example.com' #"})
			},
			ExpectedQuery: "SELECT id FROM users WHERE email = $1 AND id = $2 AND username = $3;",
			ExpectedArgs:  []any{"test@example.com' #", "1' -- ", "admin' /*"},
		},
		{
			Name: "unicode injection attempt",
			Setup: func() *SelectBuilder {
				return Select("*").
					From("users").
					Where(Eq{"username": "admin\u0027 OR \u00271\u0027=\u00271"})
			},
			ExpectedQuery: "SELECT * FROM users WHERE username = $1;",
			ExpectedArgs:  []any{"admin' OR '1'='1"},
		},
		{
			Name: "hex encoding injection",
			Setup: func() *SelectBuilder {
				return Select("id", "name").
					From("users").
					Where(Eq{"id": "1' AND 1=0 UNION ALL SELECT NULL,NULL,NULL,NULL,NULL FROM dual WHERE ''='"})
			},
			ExpectedQuery: "SELECT id, name FROM users WHERE id = $1;",
			ExpectedArgs:  []any{"1' AND 1=0 UNION ALL SELECT NULL,NULL,NULL,NULL,NULL FROM dual WHERE ''='"},
		},
		{
			Name: "time-based blind injection attempt",
			Setup: func() *SelectBuilder {
				return Select("id").
					From("users").
					Where(Eq{"username": "admin' AND SLEEP(5) --"})
			},
			ExpectedQuery: "SELECT id FROM users WHERE username = $1;",
			ExpectedArgs:  []any{"admin' AND SLEEP(5) --"},
		},
		{
			Name: "complex injection with all comparison operators",
			Setup: func() *SelectBuilder {
				return Select("*").
					From("users").
					Where(
						Eq{"status": "'; DROP TABLE users; --"},
						Ne("role", "admin' OR '1'='1"),
						Gt("age", "18' OR '1'='1"),
						Gte("score", "50; DELETE FROM users; --"),
						Lt("balance", "1000' OR '1'='1"),
						Lte("attempts", "3'; DROP TABLE sessions; --"),
					)
			},
			ExpectedQuery: "SELECT * FROM users WHERE status = $1 AND role <> $2 AND age > $3 AND score >= $4 AND balance < $5 AND attempts <= $6;",
			ExpectedArgs:  []any{"'; DROP TABLE users; --", "admin' OR '1'='1", "18' OR '1'='1", "50; DELETE FROM users; --", "1000' OR '1'='1", "3'; DROP TABLE sessions; --"},
		},
		{
			Name: "injection with HAVING and GROUP BY",
			Setup: func() *SelectBuilder {
				return Select("org_id", "COUNT(*)").
					From("users").
					Where(Eq{"deleted_at": nil}).
					GroupBy("org_id").
					Having(
						Gt("COUNT(*)", "5' OR '1'='1"),
						Lt("SUM(balance)", "10000; DROP TABLE users; --"),
					)
			},
			ExpectedQuery: "SELECT org_id, COUNT(*) FROM users WHERE deleted_at IS NULL GROUP BY org_id HAVING COUNT(*) > $1 AND SUM(balance) < $2;",
			ExpectedArgs:  []any{"5' OR '1'='1", "10000; DROP TABLE users; --"},
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

// mockPaginator implements Paginator for testing.
type mockPaginator struct {
	cursor map[string]any
	limit  int
}

func (m mockPaginator) GetCursor() map[string]any { return m.cursor }
func (m mockPaginator) GetLimit() int             { return m.limit }

func TestPaginate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func() *SelectBuilder
		paginator     Paginator
		expectedQuery string
		expectedArgs  []any
	}{
		{
			name: "single column ASC cursor",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").OrderBy(Asc, "id")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"id": "123"},
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users WHERE id > $1 ORDER BY id ASC LIMIT 21;",
			expectedArgs:  []any{"123"},
		},
		{
			name: "single column DESC cursor",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").OrderBy(Desc, "created_at")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"created_at": "2024-01-01"},
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users WHERE created_at < $1 ORDER BY created_at DESC LIMIT 21;",
			expectedArgs:  []any{"2024-01-01"},
		},
		{
			name: "multi-column same direction DESC",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					OrderBy(Desc, "created_at").
					OrderBy(Desc, "id")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"created_at": "2024-01-01", "id": "123"},
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users WHERE (created_at < $1 OR (created_at = $2 AND id < $3)) ORDER BY created_at DESC, id DESC LIMIT 21;",
			expectedArgs:  []any{"2024-01-01", "2024-01-01", "123"},
		},
		{
			name: "multi-column mixed directions DESC, ASC",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					OrderBy(Desc, "created_at").
					OrderBy(Asc, "id")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"created_at": "2024-01-01", "id": "123"},
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users WHERE (created_at < $1 OR (created_at = $2 AND id > $3)) ORDER BY created_at DESC, id ASC LIMIT 21;",
			expectedArgs:  []any{"2024-01-01", "2024-01-01", "123"},
		},
		{
			name: "nil cursor sets limit only first page",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").OrderBy(Desc, "created_at")
			},
			paginator: mockPaginator{
				cursor: nil,
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users ORDER BY created_at DESC LIMIT 21;",
			expectedArgs:  nil,
		},
		{
			name: "empty cursor sets limit only first page",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").OrderBy(Desc, "created_at")
			},
			paginator: mockPaginator{
				cursor: map[string]any{},
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users ORDER BY created_at DESC LIMIT 21;",
			expectedArgs:  nil,
		},
		{
			name: "no orderBy with cursor sets limit only",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"id": "123"},
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users LIMIT 21;",
			expectedArgs:  nil,
		},
		{
			name: "cursor missing orderBy column skips it",
			setup: func() *SelectBuilder {
				return Select("id", "name").From("users").
					OrderBy(Desc, "created_at").
					OrderBy(Asc, "id")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"created_at": "2024-01-01"}, // missing "id"
				limit:  20,
			},
			expectedQuery: "SELECT id, name FROM users WHERE created_at < $1 ORDER BY created_at DESC, id ASC LIMIT 21;",
			expectedArgs:  []any{"2024-01-01"},
		},
		{
			name: "three columns",
			setup: func() *SelectBuilder {
				return Select("*").From("events").
					OrderBy(Desc, "created_at").
					OrderBy(Desc, "priority").
					OrderBy(Asc, "id")
			},
			paginator: mockPaginator{
				cursor: map[string]any{"created_at": "2024-01-01", "priority": 5, "id": "abc"},
				limit:  10,
			},
			expectedQuery: "SELECT * FROM events WHERE (created_at < $1 OR (created_at = $2 AND priority < $3) OR (created_at = $4 AND priority = $5 AND id > $6)) ORDER BY created_at DESC, priority DESC, id ASC LIMIT 11;",
			expectedArgs:  []any{"2024-01-01", "2024-01-01", 5, "2024-01-01", 5, "abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			builder := tt.setup()
			builder.Paginate(tt.paginator)

			query, args, err := builder.Build()
			require.NoError(t, err)
			require.Equal(t, tt.expectedQuery, query)
			require.Equal(t, tt.expectedArgs, args)
		})
	}
}
