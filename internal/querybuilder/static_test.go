package querybuilder

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"nautilus/internal/testutil/require"
)

// recoverMsg runs fn and returns the panic value formatted as a string. It
// returns an empty string when fn does not panic.
func recoverMsg(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("%v", r)
		}
	}()
	fn()
	return ""
}

// Pure-Param cases across all four builders, each on both dialects.
func TestStatic_PureParam_AllBuilders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name             string
		Dialect          PlaceholderDialect
		Build            func(d PlaceholderDialect, key string) *StaticQuery
		ExpectedSQL      string
		ExpectedCount    int
		ExpectedArgInput []any
		ExpectedArgs     []any
	}{
		{
			Name:    "select postgres single param",
			Dialect: DialectPostgres,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Select("id", "name").From("users").
					Where(Eq{"id": Param(1)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "SELECT id, name FROM users WHERE id = $1;",
			ExpectedCount:    1,
			ExpectedArgInput: []any{int64(7)},
			ExpectedArgs:     []any{int64(7)},
		},
		{
			Name:    "select sqlite single param",
			Dialect: DialectSQLite,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Select("id", "name").From("users").
					Where(Eq{"id": Param(1)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "SELECT id, name FROM users WHERE id = ?;",
			ExpectedCount:    1,
			ExpectedArgInput: []any{int64(7)},
			ExpectedArgs:     []any{int64(7)},
		},
		{
			Name:    "select postgres multiple params",
			Dialect: DialectPostgres,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Select("*").From("orders").
					Where(Eq{"user_id": Param(1), "status": Param(2)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "SELECT * FROM orders WHERE status = $2 AND user_id = $1;",
			ExpectedCount:    2,
			ExpectedArgInput: []any{42, "shipped"},
			ExpectedArgs:     []any{42, "shipped"},
		},
		{
			Name:    "select sqlite multiple params",
			Dialect: DialectSQLite,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Select("*").From("orders").
					Where(Eq{"user_id": Param(1), "status": Param(2)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "SELECT * FROM orders WHERE status = ? AND user_id = ?;",
			ExpectedCount:    2,
			ExpectedArgInput: []any{42, "shipped"},
			ExpectedArgs:     []any{42, "shipped"},
		},
		{
			Name:    "update postgres",
			Dialect: DialectPostgres,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Update("users").
					Set("name", Param(1), "email", Param(2)).
					Where(Eq{"id": Param(3)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "UPDATE users SET name = $1, email = $2 WHERE id = $3;",
			ExpectedCount:    3,
			ExpectedArgInput: []any{"alice", "a@b.com", 1},
			ExpectedArgs:     []any{"alice", "a@b.com", 1},
		},
		{
			Name:    "update sqlite",
			Dialect: DialectSQLite,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Update("users").
					Set("name", Param(1), "email", Param(2)).
					Where(Eq{"id": Param(3)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "UPDATE users SET name = ?, email = ? WHERE id = ?;",
			ExpectedCount:    3,
			ExpectedArgInput: []any{"alice", "a@b.com", 1},
			ExpectedArgs:     []any{"alice", "a@b.com", 1},
		},
		{
			Name:    "delete postgres",
			Dialect: DialectPostgres,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Delete("sessions").
					Where(Eq{"user_id": Param(1)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "DELETE FROM sessions WHERE user_id = $1;",
			ExpectedCount:    1,
			ExpectedArgInput: []any{99},
			ExpectedArgs:     []any{99},
		},
		{
			Name:    "delete sqlite",
			Dialect: DialectSQLite,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Delete("sessions").
					Where(Eq{"user_id": Param(1)}).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "DELETE FROM sessions WHERE user_id = ?;",
			ExpectedCount:    1,
			ExpectedArgInput: []any{99},
			ExpectedArgs:     []any{99},
		},
		{
			Name:    "insert postgres",
			Dialect: DialectPostgres,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Insert("users").
					Set("name", Param(1), "email", Param(2)).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "INSERT INTO users (name, email) VALUES ($1, $2);",
			ExpectedCount:    2,
			ExpectedArgInput: []any{"bob", "b@c.com"},
			ExpectedArgs:     []any{"bob", "b@c.com"},
		},
		{
			Name:    "insert sqlite",
			Dialect: DialectSQLite,
			Build: func(d PlaceholderDialect, key string) *StaticQuery {
				return Insert("users").
					Set("name", Param(1), "email", Param(2)).
					Dialect(d).Static(key)
			},
			ExpectedSQL:      "INSERT INTO users (name, email) VALUES (?, ?);",
			ExpectedCount:    2,
			ExpectedArgInput: []any{"bob", "b@c.com"},
			ExpectedArgs:     []any{"bob", "b@c.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			q := tt.Build(tt.Dialect, t.Name())
			require.Equal(t, tt.ExpectedSQL, q.SQL())
			require.Equal(t, tt.ExpectedCount, q.ParamCount())

			sql, args := q.Query(tt.ExpectedArgInput...)
			require.Equal(t, tt.ExpectedSQL, sql)
			require.Equal(t, tt.ExpectedArgs, args)
		})
	}
}

// Trivial constant SQL: no Param, no concrete values. Both paramCount and
// the args slice should be empty.
func TestStatic_TrivialConstant(t *testing.T) {
	t.Parallel()

	t.Run("select count(*)", func(t *testing.T) {
		t.Parallel()
		q := Select("COUNT(*)").From("users").Static(t.Name())
		require.Equal(t, "SELECT COUNT(*) FROM users;", q.SQL())
		require.Equal(t, 0, q.ParamCount())

		sql, args := q.Query()
		require.Equal(t, "SELECT COUNT(*) FROM users;", sql)
		require.Empty(t, args)
	})

	t.Run("delete with IS NULL only", func(t *testing.T) {
		t.Parallel()
		q := Delete("users").Where(Eq{"deleted_at": nil}).Static(t.Name())
		require.Equal(t, "DELETE FROM users WHERE deleted_at IS NULL;", q.SQL())
		require.Equal(t, 0, q.ParamCount())

		sql, args := q.Query()
		require.Equal(t, "DELETE FROM users WHERE deleted_at IS NULL;", sql)
		require.Empty(t, args)
	})
}

// IS NULL conditions consume no Param slot and should not affect paramCount.
func TestStatic_NullConditionWithParam(t *testing.T) {
	t.Parallel()

	q := Select("id", "name").From("users").
		Where(Eq{"id": Param(1), "deleted_at": nil}).
		Static(t.Name())

	require.Equal(t, "SELECT id, name FROM users WHERE deleted_at IS NULL AND id = $1;", q.SQL())
	require.Equal(t, 1, q.ParamCount())

	sql, args := q.Query(42)
	require.Equal(t, "SELECT id, name FROM users WHERE deleted_at IS NULL AND id = $1;", sql)
	require.Equal(t, []any{42}, args)
}

// Postgres reuse: Param(1) referenced in two places binds the same arg twice.
func TestStatic_PostgresParamReuse(t *testing.T) {
	t.Parallel()

	q := Select("id").From("audit_log").
		Where(
			Eq{"created_by": Param(1)},
			Eq{"updated_by": Param(1)},
		).
		Static(t.Name())

	sql := q.SQL()
	require.Equal(t, 1, q.ParamCount())
	require.Equal(t, 2, strings.Count(sql, "$1"))
	require.Equal(t, "SELECT id FROM audit_log WHERE created_by = $1 AND updated_by = $1;", sql)

	gotSQL, gotArgs := q.Query("user-123")
	require.Equal(t, sql, gotSQL)
	require.Equal(t, []any{"user-123"}, gotArgs)
}

// Multi-section query: INSERT with VALUES + ON CONFLICT DO UPDATE SET, all
// using Param at distinct positions. Verifies Param tracking propagates
// through every section of the renderer.
func TestStatic_InsertOnConflictMultiSection(t *testing.T) {
	t.Parallel()

	q := Insert("users").
		Columns("id", "name", "email").
		Values(Param(1), Param(2), Param(3)).
		OnConflict("id").
		DoUpdateSet("name", Param(2), "email", Param(3)).
		Static(t.Name())

	require.Equal(t, 3, q.ParamCount())
	require.Equal(t,
		"INSERT INTO users (id, name, email) VALUES ($1, $2, $3) ON CONFLICT (id) DO UPDATE SET name = $2, email = $3;",
		q.SQL(),
	)

	sql, args := q.Query(1, "carol", "c@d.com")
	require.Equal(t, q.SQL(), sql)
	require.Equal(t, []any{1, "carol", "c@d.com"}, args)
}

// Static() must reject any chain that contains a concrete (non-Param) value
// in any value-bearing slot, on any builder.
func TestStatic_RejectsConcreteValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name  string
		Build func(key string) *StaticQuery
	}{
		{
			Name: "select where concrete",
			Build: func(key string) *StaticQuery {
				return Select("id").From("users").Where(Eq{"id": 5}).Static(key)
			},
		},
		{
			Name: "select where mixed param + concrete",
			Build: func(key string) *StaticQuery {
				return Select("id").From("users").
					Where(Eq{"id": Param(1), "tenant_id": "abc"}).
					Static(key)
			},
		},
		{
			Name: "update set concrete",
			Build: func(key string) *StaticQuery {
				return Update("users").Set("name", "alice").
					Where(Eq{"id": Param(1)}).
					Static(key)
			},
		},
		{
			Name: "update where concrete",
			Build: func(key string) *StaticQuery {
				return Update("users").Set("name", Param(1)).
					Where(Eq{"id": 5}).
					Static(key)
			},
		},
		{
			Name: "delete where concrete",
			Build: func(key string) *StaticQuery {
				return Delete("users").Where(Eq{"id": 5}).Static(key)
			},
		},
		{
			Name: "insert values concrete",
			Build: func(key string) *StaticQuery {
				return Insert("users").Columns("id", "name").Values(1, "x").Static(key)
			},
		},
		{
			Name: "insert set concrete",
			Build: func(key string) *StaticQuery {
				return Insert("users").Set("name", "x").Static(key)
			},
		},
		{
			Name: "insert do update set concrete",
			Build: func(key string) *StaticQuery {
				return Insert("users").
					Columns("id", "name").
					Values(Param(1), Param(2)).
					OnConflict("id").
					DoUpdateSet("name", "fallback").
					Static(key)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			msg := recoverMsg(func() { _ = tt.Build(t.Name()) })
			require.NotEmpty(t, msg, "expected panic")
			require.Contains(t, msg, ".Static() requires Param(N) for every value slot")
		})
	}
}

// Static() must reject Param numbering with gaps (e.g. Param(1) and Param(3)
// without Param(2)). The error message names the missing index.
func TestStatic_RejectsParamGap(t *testing.T) {
	t.Parallel()

	build := func() *StaticQuery {
		return Select("id").From("users").
			Where(Eq{"id": Param(1), "tenant_id": Param(3)}).
			Static(t.Name())
	}

	msg := recoverMsg(func() { _ = build() })
	require.NotEmpty(t, msg, "expected panic")
	require.Contains(t, msg, "Param(2) missing")
}

// Query() must panic when the caller supplies the wrong number of args.
func TestStatic_QueryArgCountMismatch(t *testing.T) {
	t.Parallel()

	q := Select("id").From("users").
		Where(Eq{"id": Param(1), "tenant_id": Param(2)}).
		Static(t.Name())

	t.Run("too few", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _, _ = q.Query(1) })
	})

	t.Run("too many", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _, _ = q.Query(1, 2, 3) })
	})

	t.Run("none when expected", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _, _ = q.Query() })
	})
}

// Build() must return an error (not panic) when called on a chain that
// contains a Param sentinel. This is the safety net for callers who forget
// the terminal .Static().
func TestBuild_RejectsParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name  string
		Build func() (string, []any, error)
	}{
		{
			Name: "select",
			Build: func() (string, []any, error) {
				return Select("id").From("users").Where(Eq{"id": Param(1)}).Build()
			},
		},
		{
			Name: "update",
			Build: func() (string, []any, error) {
				return Update("users").Set("name", Param(1)).Where(Eq{"id": Param(2)}).Build()
			},
		},
		{
			Name: "delete",
			Build: func() (string, []any, error) {
				return Delete("users").Where(Eq{"id": Param(1)}).Build()
			},
		},
		{
			Name: "insert set",
			Build: func() (string, []any, error) {
				return Insert("users").Set("name", Param(1)).Build()
			},
		},
		{
			Name: "insert values",
			Build: func() (string, []any, error) {
				return Insert("users").Columns("name").Values(Param(1)).Build()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			sql, args, err := tt.Build()
			require.Error(t, err)
			require.Contains(t, err.Error(), "Param(N) requires .Static()")
			require.Empty(t, sql)
			require.Empty(t, args)
		})
	}
}

// Static() should panic with a clear message when the underlying builder is
// in an invalid state (missing table, no fields, etc.).
func TestStatic_PropagatesBuilderError(t *testing.T) {
	t.Parallel()

	t.Run("select no table", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _ = Select("id").Static(t.Name()) })
	})

	t.Run("delete no where", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _ = Delete("users").Static(t.Name()) })
	})

	t.Run("update no sets", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _ = Update("users").Static(t.Name()) })
	})
}

// SQL() and ParamCount() are read-only accessors; calling them many times
// must return identical results.
func TestStatic_AccessorsAreStable(t *testing.T) {
	t.Parallel()

	q := Select("id").From("users").Where(Eq{"id": Param(1)}).Static(t.Name())

	sql1, count1 := q.SQL(), q.ParamCount()
	sql2, count2 := q.SQL(), q.ParamCount()

	require.Equal(t, sql1, sql2)
	require.Equal(t, count1, count2)
	require.Equal(t, 1, count1)
}

// Repeated .Static(key) calls from inline chains must return the same
// underlying *StaticQuery and must not re-run buildOnce. This is the whole
// point of moving the cache global: callers can inline the chain at every
// call site and pay the build cost exactly once per key.
func TestStatic_CachesByKey(t *testing.T) {
	t.Parallel()

	key := t.Name()

	q1 := Select("id").From("users").Where(Eq{"id": Param(1)}).Static(key)
	q2 := Select("id").From("users").Where(Eq{"id": Param(1)}).Static(key)

	require.Same(t, q1, q2)
}

// Different builders sharing the same key resolve to the first entry written.
// We don't validate equivalence of the chains because (a) doing so would
// require building both sides and (b) the cache key is the caller's promise
// of identity. This test pins the documented "first writer wins" semantic so
// it doesn't silently regress to e.g. "last writer wins".
func TestStatic_FirstWriterWinsOnKeyCollision(t *testing.T) {
	t.Parallel()

	key := t.Name()

	first := Select("id").From("users").Where(Eq{"id": Param(1)}).Static(key)
	second := Select("name").From("accounts").Where(Eq{"id": Param(1)}).Static(key)

	require.Same(t, first, second)
	require.Equal(t, "SELECT id FROM users WHERE id = $1;", second.SQL())
}

// Concurrent first-call from many goroutines must converge on a single
// cached *StaticQuery without panicking or corrupting state.
func TestStatic_ConcurrentFirstCall(t *testing.T) {
	t.Parallel()

	key := t.Name()
	const goroutines = 32

	results := make([]*StaticQuery, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = Select("id").From("users").
				Where(Eq{"id": Param(1)}).
				Static(key)
		}(i)
	}
	wg.Wait()

	for i := 1; i < goroutines; i++ {
		require.Same(t, results[0], results[i])
	}
}

// Hot-path benchmarks. The four benchmarks below all produce the same
// (sql, args) pair for the same logical query
//
//	SELECT id, name, email FROM users WHERE tenant_id = $2 AND id = $1
//
// They differ only in how the SQL string is materialized. Package-level
// sinks force the compiler to keep the call site live, otherwise it would
// inline the static path to nothing and the numbers would be meaningless.
var (
	benchSinkSQL  string
	benchSinkArgs []any
)

const benchRawSQL = "SELECT id, name, email FROM users WHERE tenant_id = $2 AND id = $1;"

// rawVariadic mimics the calling convention of StaticQuery.Query so the
// comparison is apples-to-apples: a constant SQL string and a variadic args
// slice. This is the cheapest possible "I already know my SQL" wrapper and
// is the floor any builder-derived API should aim to match.
func rawVariadic(args ...any) (string, []any) {
	return benchRawSQL, args
}

// BenchmarkStatic_Query is the hot path for callers that hoist the
// *StaticQuery out of the loop. Expected: 1 alloc/op (the variadic slice),
// no per-call string building.
func BenchmarkStatic_Query(b *testing.B) {
	q := Select("id", "name", "email").From("users").
		Where(Eq{"id": Param(1), "tenant_id": Param(2)}).
		Static("bench:static_query")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkSQL, benchSinkArgs = q.Query(i, "tenant")
	}
}

// BenchmarkStatic_InlineQuery models the intended hot-path usage: the chain
// is constructed inline at every call site and the cache absorbs the SQL
// build cost. Captures both the builder allocations and the cache lookup
// overhead so we can track the gap against the hoisted variant above.
func BenchmarkStatic_InlineQuery(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkSQL, benchSinkArgs = Select("id", "name", "email").From("users").
			Where(Eq{"id": Param(1), "tenant_id": Param(2)}).
			Static("bench:static_inline").
			Query(i, "tenant")
	}
}

// BenchmarkRawSQL_Variadic is the floor: a function with the same shape as
// StaticQuery.Query that simply hands back a constant string and the
// variadic args slice. The static path should be within a few ns of this.
func BenchmarkRawSQL_Variadic(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkSQL, benchSinkArgs = rawVariadic(i, "tenant")
	}
}

// BenchmarkRawSQL_Inline simulates the typical raw-SQL call site: a constant
// string plus an inline slice literal at the call site (e.g. what you'd
// pass to db.QueryContext). Same 1 alloc, same cost profile.
func BenchmarkRawSQL_Inline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkSQL = benchRawSQL
		benchSinkArgs = []any{i, "tenant"}
	}
}

// BenchmarkBuild_Equivalent shows the cost of dynamic Build() on the same
// logical query for context. Expected: many allocations relative to static.
func BenchmarkBuild_Equivalent(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sql, args, _ := Select("id", "name", "email").From("users").
			Where(Eq{"id": i, "tenant_id": "tenant"}).
			Build()
		benchSinkSQL = sql
		benchSinkArgs = args
	}
}
