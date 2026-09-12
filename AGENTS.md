# AGENTS.md

Repository guidance for AI coding assistants working on this Go application.

The project Codex default is GPT-6 Astra; see `.codex/config.toml`.

## Collaboration

- Treat action requests as work to complete. Infer routine details from context and carry implementation, validation, and delivery through to completion.
- Ask when missing information materially changes scope, correctness, or an irreversible decision. Continue independent authorized work while waiting, and prepare a concrete, reviewable result before requesting any outstanding approval. Do not ask again for authorization already given.
- Incorporate corrections and answer side questions while preserving the active objective unless the user cancels or replaces it.
- Use available subagents for independent work when they improve speed or quality. Give each a bounded task, review its results, and integrate them before declaring completion.
- Prefer GPT-6 Astra for subagents, with low reasoning for exploration and narrowly scoped edits, and medium for implementation or review. Preserve the main agent's selected reasoning effort.
- Lead with the outcome in concise, plain prose. Report meaningful progress, validation, and remaining blockers; use lists when they improve clarity and avoid stock phrases or unnecessary jargon.

## Skills

- Use `.agents/skills/` when the task needs its project-specific workflow. Read the selected `SKILL.md` and only relevant supporting references; do not load adjacent skills merely because they share a language or directory. Explicit user instructions take precedence over skill guidelines.
- If a skill causes a pause, approval request, or departure from the user's intent, link the exact `SKILL.md`, quote the instruction, and explain its applicability. Distinguish an explicit requirement from your interpretation; routine implementation choices do not create approval requirements.
- Keep skills focused on repository knowledge that changes a decision. Remove stale workflows and generic tutorials; keep shared validation and delivery rules here.

## Development

- Use `dotenvx run --` when a command needs project environment variables. Use `dotenvx set` rather than editing encrypted values by hand.
- Use `docker compose up -d` when the full local stack is needed. Go integration tests require a running Docker daemon, not necessarily the Compose stack.

## Dependencies

- Prefer the Go standard library. Add a production dependency only when it clearly reduces complexity and no existing dependency fits.

## Go

- Prefer the smallest clear implementation. Preserve real domain, I/O, and concurrency boundaries; do not introduce abstractions for hypothetical reuse or mocking alone.
- Keep service interfaces at real consumer or provider boundaries, with domain types rather than provider SDK types. Put `context.Context` first for scoped work; follow existing pointer-options conventions, using `optional.Optional[T]` when omission differs from zero.
- Never alias a package import unless an unavoidable name collision remains after considering the package or test-package structure.
- Use `nautilus/internal/errors`; wrap a failure once with safe context where it originates. Keep sensitive values out of errors and logs. Follow `backend-http-errors` for public handler responses and error codes.
- Follow `backend-tests` for Go tests. Use table-driven tests for multiple cases, direct tests for one behavior, and assert observable contracts.
- Follow `backend-form-handling` for request forms and `entity-mux-registration` for routes and handler organization.
- Follow `database-schema` and `database-queries` for database work. Keep business validation in Go; never add application-defined SQL functions, stored procedures, triggers, `CHECK` constraints, or schema-level business validation.
- Write SQL column lists directly in each query. Never concatenate shared column-list constants or variables into SQL (such as `+ columns`); keep fixed queries as complete SQL literals.
- Keep atomicity- and concurrency-sensitive work in SQL, including upserts, compare-and-swap writes, row locking, leasing, idempotency, and set-based relational work.
- Go filenames must not contain underscores except for `_test.go` files.
- Prefer `any` over `interface{}`.
- Use conventional Go initialisms and short local names. Add comments for non-obvious constraints, and preserve data integrity, cancellation, and tenant boundaries when simplifying code.
- Do not leave build artifacts in the repository.

## Validation

- Run the smallest relevant checks while iterating, then the required checks before handoff.
- Add tests for meaningful behavior and regressions. Avoid tests that merely copy the implementation of a reversible, low-impact change.
- For Go changes, run `dotenvx run -- go test ./...` and `dotenvx run -- golangci-lint run --new-from-rev=origin/main`. In Codex, run the linter with elevated permissions.
- For documentation-only changes, run `git diff --check`.
- After required checks pass, repeat or broaden validation only for new changes, failures, or unresolved concerns. Report checks that could not run and why.

## Delivery

- For substantive changes, use a branch prefixed with the user opening it, such as `cameron/`. Commit and push the change, open a GitHub PR, and merge after required checks pass.
- After merging, fetch and rebase local `main` onto `origin/main`, preserving unrelated user work.
