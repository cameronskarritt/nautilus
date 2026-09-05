---
name: backend-tests
description: Write and review Go tests that follow project conventions. Use when adding or changing _test.go files, table-driven unit tests, handler tests, database tests, error assertions, test fixtures, or cleanup and parallelism. Use the internal testutil/require wrapper, isolate cases, test observable contracts and boundaries, and run focused plus repository-wide validation.
---

# Backend Tests

## Choose the Test Shape

Use one direct test for one behavior. Use a table when several inputs exercise
the same contract:

```go
func TestParseLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "minimum", input: "1", want: 1},
		{name: "maximum", input: "100", want: 100},
		{name: "empty", wantErr: true},
		{name: "above maximum", input: "101", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseLimit(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
```

Name cases for behavior or boundaries, not implementation steps. Cover the
minimum, maximum, zero, empty, malformed, and adjacent out-of-range values when
they define the contract.

## Use Project Assertions

Import the internal wrapper:

```go
import "nautilus/internal/testutil/require"
```

Use the narrowest meaningful assertion:

```go
require.NoError(t, err)
require.Error(t, err)
require.ErrorIs(t, err, expected)
require.ErrorAs(t, err, &target)
require.Equal(t, want, got)
require.JSONEq(t, wantJSON, gotJSON)
```

Prefer `ErrorIs` or `ErrorAs` for error contracts. Assert exact text only when
the message is itself a public contract. Assert HTTP status, public error code,
and field together when they are client-visible behavior.

Call `t.Helper()` in shared assertion and fixture helpers.

## Apply Parallelism Safely

Call `t.Parallel()` for independent tests and subtests. Do not use it when a
test mutates process-global state, shares a non-isolated fixture, or relies on
ordering.

Use `t.Setenv` for environment variables and keep that test and its ancestors
non-parallel:

```go
func TestFromEnv(t *testing.T) {
	t.Setenv("FEATURE_MODE", "enabled")
	// ...
}
```

Use `t.Cleanup` for servers, files, goroutines, and other resources whose
lifetime should match the test:

```go
server := httptest.NewServer(handler)
t.Cleanup(server.Close)
```

Use `t.Context()` for operations that should stop with the test.

## Select the Package

Use an external test package for handler and public API behavior:

```go
package widgets_test
```

Use the package under test only when verifying unexported behavior is necessary.
Do not alias imports to work around an avoidable test-package collision; choose
the appropriate test package instead.

Name files `*_test.go` and functions `TestFunction` or `TestType_Method`.

## Test Database Code

Use `testutil.SetupTestDB(t)` for transaction-isolated database tests. Cleanup
and rollback are registered automatically.

```go
func TestCreate(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	got, err := widgets.Create(t.Context(), db, "name")
	require.NoError(t, err)
	require.NotNil(t, got)
}
```

Use `testutil.SetupTestDBWithCommit(t)` only when testing real commit behavior
or transaction semantics. Use `testutil.CreateTestUser` for user fixtures and
give multiple users distinct suffixes.

Assert both the returned model and relevant persisted state. Cover uniqueness,
not-found, pagination, and partial-update boundaries where applicable.

## Test HTTP Handlers

Use `httptest.NewRequest` and `httptest.NewRecorder`. Add required context values
through the domain helpers:

```go
req := httptest.NewRequest(http.MethodGet, "/widgets/"+id, nil)
req = req.WithContext(users.WithContext(req.Context(), user))
rec := httptest.NewRecorder()

mux.Get(rec, req)

require.Equal(t, http.StatusOK, rec.Code)
```

For errors, assert the status and decode the public response. For success,
assert meaningful response fields rather than only checking that JSON decodes.

## Validate

Run the smallest relevant package first:

```bash
dotenvx run -- go test ./internal/path/to/package
```

Run a focused test while iterating when useful:

```bash
dotenvx run -- go test ./internal/path/to/package -run '^TestName$'
```

Before handoff, follow the Go validation gates in `AGENTS.md`.

## Review Checklist

- Test observable behavior and boundary values.
- Keep cases isolated and deterministic.
- Use table tests only for a shared contract.
- Use `testutil/require` and precise assertions.
- Avoid unsafe parallelism around global or shared state.
- Register cleanup with the test.
- Use the correct test package.
- Assert complete public HTTP error contracts.
- Run focused tests, then the required full validation.
