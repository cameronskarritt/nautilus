---
name: http-cassette-recording
description: Record and replay deterministic HTTP fixtures with the repository cassette transports. Use when testing HTTP clients against captured responses, refreshing JSONL cassettes, covering streamed SSE responses, or reviewing cassette safety, matching, and test isolation.
---

# HTTP Cassette Recording

Use `internal/testutil.Cassette` with the transports in
`internal/httputil/transport.go`. Keep replay tests offline and deterministic.
Treat cassette JSONL as committed source: synthetic, reviewable, and free of
secrets or personal data.

## Choose the Test Boundary

- Use a cassette to test a real client request/response contract: request
  serialization, response parsing, streaming, and provider-specific behavior.
- Use an in-memory `http.RoundTripper` or `httptest.Server` when a response can
  be expressed clearly in the test. Prefer these for retries, timeouts,
  cancellations, transport errors, malformed data, and branching logic.
- A recorded stream read error is not preserved: replay serves the captured
  chunks and then returns `io.EOF`. Use a scripted transport when the client
  must retry on a non-EOF read error. Use a cassette only when premature EOF is
  itself the contract under test.
- Assert domain behavior in the test. A recorded provider response is a fixture,
  not the assertion.
- Never record automatically because a fixture is missing, and never make live
  provider calls in an ordinary test or CI run.

## Inspect the Client Before Recording

Identify:

1. every method and full URL the scenario calls;
2. whether the same method and URL occurs more than once;
3. whether the response uses `Content-Type: text/event-stream`;
4. which request, response, query, and stream fields could be sensitive; and
5. the smallest synthetic request that exercises the behavior.

The replay matcher compares only the HTTP method and exact URL string. It does
not match request headers or bodies. Two `POST` requests to the same URL are
distinguished only by their order when replay uses `WithConsume()`.

## Record Deliberately

`testutil.NewCassette(path)` creates or truncates `path` immediately. Record to a
new temporary fixture name, review it, then replace the tracked fixture through
the normal diff rather than recording directly over the only known-good copy.
Create the destination directory first when necessary.

Use a custom recorder with non-production accounts and synthetic data:

```go
cassette, err := testutil.NewCassette(path)
if err != nil {
	return err
}

transport := httputil.NewRecordingTransport(http.DefaultTransport, cassette)
client := &http.Client{Transport: transport}

// Exercise the complete interaction. Drain streams before closing them.

if err := cassette.Close(); err != nil {
	return err
}
```

The non-streaming recorder buffers the complete response body. For SSE, it
records bytes only as the caller reads the response body, so drain the stream
and check its terminal error before closing the cassette.

## Review Before Commit

The recorder automatically redacts only these headers:

- `Authorization`
- `X-Api-Key`
- `Api-Key`
- `Cookie`
- `Set-Cookie`
- `OpenAI-Organization`
- `OpenAI-Project`
- `Anthropic-Organization-Id`
- `X-Request-Id`
- `Request-Id`
- `Cf-Ray`

It does **not** redact URL query values, request bodies, response bodies, or SSE
chunks. Inspect every JSONL line before staging. Remove or replace credentials,
tokens, signatures, personal data, proprietary prompts, and unstable
identifiers while preserving valid payload syntax. Automated secret scanning
is useful but does not replace manual review.

Do not commit a fixture captured from production or customer traffic. If safe
sanitization would change the behavior being tested, construct the fixture
locally instead of recording it.

## Replay in Tests

Use one freshly loaded cassette per test:

```go
func TestCompletion(t *testing.T) {
	t.Parallel()

	cassette, err := testutil.LoadCassette(
		"testdata/completion_text.jsonl",
	)
	require.NoError(t, err)

	transport := httputil.NewReplayTransport(
		cassette,
		httputil.WithFastForward(),
	)
	client := NewClient(nil).
		WithAPIKey("fake-key").
		WithTransport(transport)

	msg, err := client.Completion(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, enums.RoleAssistant, msg.Role)
	require.NotEqual(t, "", msg.Content)
}
```

`WithFastForward()` removes recorded delays between streamed chunks. Use it by
default; omit it only in a bounded test whose subject is replay timing.

Use `WithConsume()` when a single scenario makes ordered calls with the same
method and URL:

```go
transport := httputil.NewReplayTransport(
	cassette,
	httputil.WithFastForward(),
	httputil.WithConsume(),
)
```

Without consumption, every such call receives the first matching response.
With consumption, calls receive matching interactions in cassette order and
then fail with `httputil.ErrNoMatchingInteraction`. Because bodies are ignored,
do not use consumption to model concurrent same-URL requests whose order is
nondeterministic.

If serialized request content is part of the contract, wrap the replay
transport in a test-local observer that reads and restores the outbound body
before delegating, then assert the decoded body and relevant headers. Inspecting
the request stored in the fixture verifies only the fixture, not what the
current client sent. A replay success alone cannot detect swapped or malformed
bodies when method and URL still match.

Never share a consuming cassette between parallel tests. Separate fixture files
are the clearest isolation; loading the same immutable fixture independently is
safe. For an ordered scenario, assert that no expected interactions remain:

```go
require.Len(t, cassette.Interactions, 0)
```

## Understand the JSONL

A non-streaming response is one line:

```json
{"type":"interaction","request":{...},"response":{...}}
```

An SSE response begins with a `stream` line followed by `event` lines:

```json
{"type":"stream","request":{...},"response":{"status":200,"header":{...}}}
{"type":"event","offset":96459,"data":"event: response.created\ndata: {...}\n\n"}
```

Despite the field name, an `event` line is a body read chunk with a nanosecond
offset, not necessarily one semantic SSE event. Tests should assert parsed
client output, not chunk boundaries.

## Verify

Run the focused transport and client suites:

```bash
dotenvx run -- go test ./internal/httputil \
  ./internal/ai/llm/openai \
  ./internal/ai/llm/anthropic
```

Then inspect the staged JSONL diff again. A replay pass proves determinism; it
does not prove that a fixture is safe to commit. Follow the Go validation gates
in `AGENTS.md` before handoff.
