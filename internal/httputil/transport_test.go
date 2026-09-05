package httputil

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRecordingTransport(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	cassette, err := testutil.NewCassette(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cassette.Close()) })

	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"prompt":"hello"}`, string(body))

		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":        {"application/json"},
				"Openai-Organization": {"org_live"},
				"X-Request-Id":        {"req_live"},
			},
			Body: io.NopCloser(bytes.NewBufferString(`{"message":"hello"}`)),
		}, nil
	})
	transport := NewRecordingTransport(base, cassette)

	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/messages", bytes.NewBufferString(`{"prompt":"hello"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "secret")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"message":"hello"}`, string(body))
	require.Len(t, cassette.Interactions, 1)

	got := cassette.Interactions[0]
	require.JSONEq(t, `{"prompt":"hello"}`, string(got.Request.Body))
	require.Equal(t, "[REDACTED]", got.Request.Header.Get("Authorization"))
	require.Equal(t, "[REDACTED]", got.Response.Header.Get("Openai-Organization"))
	require.Equal(t, "[REDACTED]", got.Response.Header.Get("X-Request-Id"))
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	keys := []string{
		"Authorization",
		"X-Api-Key",
		"Api-Key",
		"Cookie",
		"Set-Cookie",
		"OpenAI-Organization",
		"OpenAI-Project",
		"Anthropic-Organization-Id",
		"X-Request-Id",
		"Request-Id",
		"Cf-Ray",
	}
	header := make(http.Header)
	for _, key := range keys {
		header.Set(key, "secret")
	}
	header.Set("Content-Type", "application/json")

	got := redactHeaders(header)
	for _, key := range keys {
		require.Equal(t, "[REDACTED]", got.Get(key))
		require.Equal(t, "secret", header.Get(key))
	}
	require.Equal(t, "application/json", got.Get("Content-Type"))
}

func TestRecordingTransportSSE(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	cassette, err := testutil.NewCassette(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cassette.Close()) })

	want := "event: message\ndata: first\n\nevent: message\ndata: second\n\n"
	base := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString(want)),
		}, nil
	})
	transport := NewRecordingTransport(base, cassette)
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/stream", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, want, string(body))
	require.NoError(t, cassette.Close())

	loaded, err := testutil.LoadCassette(path)
	require.NoError(t, err)
	require.Len(t, loaded.Interactions, 1)
	require.Len(t, loaded.Interactions[0].Response.Events, 1)
	require.Equal(t, want, loaded.Interactions[0].Response.Events[0].Data)
}

func TestReplayTransport(t *testing.T) {
	t.Parallel()

	cassette := &testutil.Cassette{Interactions: []testutil.Interaction{{
		Request: testutil.RecordedRequest{
			Method: http.MethodPost,
			URL:    "https://api.example.com/messages",
		},
		Response: testutil.RecordedResponse{
			Status: http.StatusCreated,
			Header: http.Header{"Content-Type": {"application/json"}},
			Body:   []byte(`{"id":123}`),
		},
	}}}
	transport := NewReplayTransport(cassette)
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/messages", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	require.JSONEq(t, `{"id":123}`, string(body))
}

func TestReplayTransportSSE(t *testing.T) {
	t.Parallel()

	cassette := &testutil.Cassette{Interactions: []testutil.Interaction{{
		Request: testutil.RecordedRequest{
			Method: http.MethodGet,
			URL:    "https://api.example.com/stream",
		},
		Response: testutil.RecordedResponse{
			Status: http.StatusOK,
			Events: []testutil.SSEEvent{
				{Data: "data: first\n\n"},
				{Data: "data: second\n\n"},
			},
		},
	}}}
	transport := NewReplayTransport(cassette, WithFastForward())
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/stream", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	require.Equal(t, "data: first\n\ndata: second\n\n", string(body))
}

func TestReplayTransportNoMatch(t *testing.T) {
	t.Parallel()

	transport := NewReplayTransport(new(testutil.Cassette))
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/missing", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	}
	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrNoMatchingInteraction)
}

func TestReplayTransportConsume(t *testing.T) {
	t.Parallel()

	cassette := &testutil.Cassette{Interactions: []testutil.Interaction{
		{
			Request:  testutil.RecordedRequest{Method: http.MethodGet, URL: "https://api.example.com/data"},
			Response: testutil.RecordedResponse{Status: http.StatusOK, Body: []byte(`{"call":1}`)},
		},
		{
			Request:  testutil.RecordedRequest{Method: http.MethodGet, URL: "https://api.example.com/data"},
			Response: testutil.RecordedResponse{Status: http.StatusOK, Body: []byte(`{"call":2}`)},
		},
	}}
	transport := NewReplayTransport(cassette, WithConsume())
	want := []string{`{"call":1}`, `{"call":2}`}

	for _, want := range want {
		req, err := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.JSONEq(t, want, string(body))
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	}
	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrNoMatchingInteraction)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
