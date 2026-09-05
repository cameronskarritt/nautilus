---
name: terse-idiomatic-go
description: >
  Write and review Go in this repository with terse, idiomatic, minimal code.
  Use when editing Go, creating Go APIs or tests, or when the user asks for
  "terse Go", "idiomatic Go", "minimal solution", "YAGNI", "do less",
  "shortest path", "ponytail", or less boilerplate and over-engineering.
---

# Terse Idiomatic Go

## Standard

Use [Effective Go](https://go.dev/doc/effective_go) as the language and naming
baseline. Follow `AGENTS.md` and the repository skills for project-specific
implementation rules.

Terse means high signal, not fewer lines. Prefer code that reads naturally to
another Go programmer. Do not compress clear code into clever code.

## Understand Before Editing

1. Read the files the change touches.
2. Trace the real call flow.
3. Inspect callers before changing behavior or a contract.
4. Fix the invariant once at the shared boundary when that is correct.

The smallest diff in the wrong place is not minimal; it is another bug.

## Use the Least Mechanism

Stop at the first choice that satisfies the real requirement:

1. Do not add code for a speculative need.
2. Use a direct Go construct.
3. Use the standard library.
4. Reuse a suitable repository type, helper, or pattern.
5. Use an existing dependency when it is clearer than local code.
6. Add the smallest new implementation that preserves the domain boundary.

Let explicit repository rules override this generic ordering. Do not extract a
helper merely because a few lines repeat. Wait until the repetition represents
one stable concept with one reason to change. Once two choices are correct,
prefer the clearer one with fewer concepts.

## Name Things Like Go

Make name length proportional to scope and ambiguity. Use short, familiar names
when the surrounding code already supplies the context:

- `ctx`, `db`, `tx`, `err`, `r`, `w`, `i`, `n`, `b`, `buf`
- `row`, `rows`, `user`, `userID`, `repo`, `org`, `cfg`, `result`

Reject names that restate the whole call chain:

| Avoid | Prefer when the scope is clear |
| --- | --- |
| `organizationRepositoryInstallationConfiguration` | `cfg` |
| `databaseQueryExecutionResult` | `result` |
| `authenticationCodeVerificationResult` | `code` or `valid` |
| `httpRequestContext` | `ctx` |

- Do not repeat context already supplied by the package, receiver, function, or
  type.
- Keep initialisms conventional: `ID`, `URL`, `HTTP`, `API`, `DB`, `SQL`, and
  `SHA`; write `userID`, not `userId`.
- Name receivers with one or two letters derived from the type, and use the same
  receiver name across its methods. Do not use `this`, `self`, or the full type
  name.
- Omit `Get` from simple field accessors. Keep `Get` for a real repository
  operation that fetches an entity when that matches the local package.
- Avoid package-name stutter. Let the package qualify its exported names.
- Use a longer name only when it disambiguates nearby values or survives across
  a scope large enough to need the reminder.

## Keep the Shape Plain

- Prefer plain functions and concrete types.
- Introduce an interface only for a real consumer-owned boundary or multiple
  meaningful behaviors, never solely to make mocking easier. Follow
  `backend-core-service-interfaces` for its exact shape.
- Keep packages cohesive. Do not split packages or files to imitate layers,
  isolate every type, or satisfy arbitrary size limits.
- Keep the happy path unindented. Return early on errors. Prefer a plain loop or
  conditional over a clever helper.
- Prefer useful zero values and struct literals. Add a constructor only to
  establish invariants or required dependencies. Add config or option types
  only when a present call site has become unclear.
- Avoid reflection. Use generics only when multiple real call sites share the
  same type-independent operation and the generic form is clearer than the
  concrete alternatives.
- Put `context.Context` first when an operation carries cancellation, deadlines,
  tracing, or request scope. Pass it through; do not store it in a struct or
  invent a custom context type.
- Prefer `any` over `interface{}`.
- Do not alias package imports unless a real collision cannot reasonably be
  removed by restructuring the code or test package.
- Do not require comments. When a comment is useful, explain a non-obvious
  constraint, invariant, or tradeoff instead of narrating the code.
- Do not leave speculative TODOs or abstractions in otherwise clear code.

## Follow Repository Rules

- Use `nautilus/internal/errors`. Add useful context once where an I/O,
  database, network, encoding, or external-service failure originates. Do not
  rewrap an already contextual error merely to announce another failure.
- Keep secrets, credentials, personal data, and raw sensitive payloads out of
  errors and logs.
- Follow the relevant repository skill for mechanics:
  `backend-core-service-interfaces`, `backend-form-handling`,
  `backend-http-errors`, `database-queries`, `database-schema`,
  `entity-mux-registration`, or `backend-tests`.

## Tests and Validation

- Write a direct test for one behavior.
- Use a table-driven test only when multiple cases share the same test logic.
- Add a focused regression test for non-trivial behavior. Rely on an existing
  test for trivial wiring only when it exercises the changed path.
- Use `internal/testutil/require` and follow `backend-tests`.
- Run `gofmt` on touched Go files and the narrow package test while iterating,
  then follow the Go validation gates in `AGENTS.md`.

## Preserve Real Boundaries

Never simplify away validation, authentication, authorization, secret handling,
data integrity, concurrency safety, cancellation, transactions, tenant scope,
schema correctness, caller compatibility, or explicit user requirements.

## Handoff

Lead with the outcome. Report the validation run and any deliberate omission.
Keep the handoff proportional to the change.
