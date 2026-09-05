package querybuilder

import (
	"fmt"
	"sync"
)

// paramRef is the internal sentinel produced by Param. It is detected during
// rendering and emits a placeholder for its declared position N without
// consuming a slot in the build-time args.
type paramRef struct {
	n int
}

// Param creates a placeholder for a runtime value at position n (1-indexed).
//
// Param(N) renders as dialect.Placeholder(N) (e.g. "$1" for Postgres or "?"
// for SQLite) at Build/Static time and consumes no slot in the build-time
// args. At Query() time, the caller supplies the runtime values; arg index
// N-1 fills slot $N for positional dialects.
//
// Param is only valid in a chain terminated by .Static(). Calling Build()
// directly on a chain that contains Param values returns an error to avoid
// emitting placeholders that the caller has no way to fill.
//
// Param N values must be contiguous from 1 to maxN. Reuse of the same N is
// allowed and works correctly under positional placeholder dialects (e.g.
// Postgres $1 referenced twice) but is dialect-specific behavior under
// non-positional dialects (SQLite ?); reuse with SQLite is a footgun and
// should be avoided.
func Param(n int) any {
	return paramRef{n: n}
}

// StaticQuery is a fully compiled query whose SQL is built once and cached
// under the caller-supplied key passed to <Builder>.Static(key). Subsequent
// .Static(key) calls return the cached instance without rebuilding, so it is
// safe to construct the chain inline in hot paths.
//
// StaticQuery is safe for concurrent use because it is read-only after
// construction.
type StaticQuery struct {
	sql        string
	paramCount int
}

// Query returns the cached SQL string and the supplied args. It panics if the
// number of args does not match the number of distinct Param(N) declarations
// captured at .Static() time. Panic is appropriate because mismatched counts
// are programmer errors discoverable at the call site.
func (s *StaticQuery) Query(args ...any) (string, []any) {
	if len(args) != s.paramCount {
		panic(fmt.Sprintf(
			"querybuilder: StaticQuery.Query expected %d args, got %d",
			s.paramCount, len(args),
		))
	}
	return s.sql, args
}

// SQL returns the cached SQL string. Useful for inspection and tests.
func (s *StaticQuery) SQL() string { return s.sql }

// ParamCount returns the number of args expected by Query().
func (s *StaticQuery) ParamCount() int { return s.paramCount }

// staticCache holds compiled StaticQuery objects keyed by the label supplied
// to <Builder>.Static(key). Entries are written exactly once on first miss
// (first-writer-wins) and read on every subsequent call; sync.Map is the
// right shape for this read-mostly, write-once-per-key pattern.
var staticCache sync.Map // map[string]*StaticQuery

// loadStatic returns the cached StaticQuery for key, or nil if no entry has
// been registered yet.
func loadStatic(key string) *StaticQuery {
	if v, ok := staticCache.Load(key); ok {
		return v.(*StaticQuery)
	}
	return nil
}

// storeStatic atomically associates q with key. If another goroutine wins
// the race, the value it stored is returned instead so all callers see the
// same *StaticQuery for a given key.
func storeStatic(key string, q *StaticQuery) *StaticQuery {
	actual, _ := staticCache.LoadOrStore(key, q)
	return actual.(*StaticQuery)
}

// newStaticQuery validates a buildCtx coming out of buildOnce and constructs
// the StaticQuery. It enforces the Param-only contract (no concrete values
// allowed) and the contiguous-1..N invariant for declared params.
func newStaticQuery(sql string, ctx *buildCtx) *StaticQuery {
	if len(ctx.args) > 0 {
		panic(fmt.Sprintf(
			"querybuilder: .Static() requires Param(N) for every value slot; found %d concrete values. Use Param(1)..Param(N) instead.",
			len(ctx.args),
		))
	}
	for i, set := range ctx.seen {
		if !set {
			panic(fmt.Sprintf(
				"querybuilder: .Static() Param(%d) missing; declared params must be contiguous 1..N",
				i+1,
			))
		}
	}
	return &StaticQuery{sql: sql, paramCount: ctx.maxN}
}

// staticBuildable is the contract shared by every builder whose chain can be
// frozen via .Static(). It exists so the cache lookup/build/store dance lives
// in one place rather than being duplicated across each builder type.
type staticBuildable interface {
	buildOnce() (*buildCtx, string, error)
}

// resolveStatic implements the common Static() pipeline: look up the cache,
// build on miss, validate the buildCtx, and publish the result under key.
// Build-time errors and contract violations panic, matching the existing
// .Static() semantics. Each builder's Static(key) is a thin wrapper around
// this function.
func resolveStatic(key string, b staticBuildable) *StaticQuery {
	if q := loadStatic(key); q != nil {
		return q
	}
	ctx, sql, err := b.buildOnce()
	if err != nil {
		panic("querybuilder: " + err.Error())
	}
	return storeStatic(key, newStaticQuery(sql, ctx))
}
