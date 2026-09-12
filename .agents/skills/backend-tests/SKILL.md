---
name: backend-tests
description: Use repository Go test fixtures and assertions when adding or changing Go tests, including database and handler tests.
---

# Repository Go Tests

Use `nautilus/internal/testutil/require`, not a direct third-party assertion
import. Prefer `ErrorIs`/`ErrorAs` for inspectable error contracts. Test public
behavior; use a direct test for one case and a table for shared logic across
multiple cases.

Use `t.Context()` for operations tied to the test lifetime and `t.Cleanup` for
owned resources. Independent tests may run in parallel; tests using `t.Setenv`
or other process-global mutations and their ancestors must not run in parallel.
Use an external test package for public behavior, and the package under test
when access to unexported behavior is necessary.

## Database Fixtures

- `testutil.SetupTestDB(t)` returns a transaction-isolated database and registers
  rollback/cleanup automatically.
- `testutil.SetupTestDBWithCommit(t)` is for real commits or transaction semantics.
- `testutil.CreateTestUser` creates user fixtures; give multiple users distinct
  suffixes.
- Database integration tests need a running Docker daemon; they do not require
  the full Compose stack.

Assert persisted state when it matters to the changed contract. Choose relevant
boundaries such as uniqueness, missing rows, pagination, and partial updates;
there is no requirement to add all of them for every database change.

## Handler Fixtures

Use `httptest.NewRequest` and `httptest.NewRecorder`, adding context through
domain helpers such as `users.WithContext`. Mount a real `internal/mux.Router`
when testing path parameters, method routing, or middleware: calling a handler
directly does not populate router parameters. Direct handler calls suit behavior
independent of route registration.

Assert meaningful response fields. Public errors should cover HTTP status,
public code, and applicable field. Follow AGENTS.md for validation commands.
