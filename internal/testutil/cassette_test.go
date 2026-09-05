package testutil

import (
	"bytes"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"nautilus/internal/testutil/require"
)

func TestCassetteRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	cassette, err := NewCassette(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cassette.Close()) })

	require.NoError(t, cassette.Append(Interaction{
		Request:  RecordedRequest{Method: http.MethodPost, URL: "https://api.example.com/messages", Body: []byte(`{"prompt":"hello"}`)},
		Response: RecordedResponse{Status: http.StatusOK, Body: []byte(`{"message":"hello"}`)},
	}))
	require.NoError(t, cassette.AppendStreamStart(
		RecordedRequest{Method: http.MethodGet, URL: "https://api.example.com/stream"},
		RecordedResponse{Status: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}},
	))
	require.NoError(t, cassette.AppendSSEEvent(time.Second, "data: first\n\n"))
	require.NoError(t, cassette.AppendSSEEvent(2*time.Second, "data: second\n\n"))
	require.NoError(t, cassette.Close())

	loaded, err := LoadCassette(path)
	require.NoError(t, err)
	require.Equal(t, cassette.Interactions, loaded.Interactions)
}

func TestCassetteMatch(t *testing.T) {
	t.Parallel()

	cassette := &Cassette{Interactions: []Interaction{{
		Request: RecordedRequest{
			Method: http.MethodPost,
			URL:    "https://api.example.com/messages?version=1",
			Body:   []byte(`{"prompt":"first"}`),
		},
		Response: RecordedResponse{Status: http.StatusOK},
	}}}
	tests := []struct {
		name   string
		method string
		url    string
		body   string
		match  bool
	}{
		{name: "exact", method: http.MethodPost, url: "https://api.example.com/messages?version=1", match: true},
		{name: "body ignored", method: http.MethodPost, url: "https://api.example.com/messages?version=1", body: `{"prompt":"other"}`, match: true},
		{name: "method differs", method: http.MethodGet, url: "https://api.example.com/messages?version=1"},
		{name: "query differs", method: http.MethodPost, url: "https://api.example.com/messages?version=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(tt.method, tt.url, bytes.NewBufferString(tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.match, cassette.Match(req) != nil)
		})
	}
}
