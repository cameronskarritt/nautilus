package opensearch_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"nautilus/internal/search/opensearch"
	"nautilus/internal/testutil/require"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  opensearch.Config
		ok   bool
	}{
		{name: "http", cfg: opensearch.Config{URL: "http://localhost:9200", Index: "documents"}, ok: true},
		{name: "https root", cfg: opensearch.Config{URL: "https://localhost/", Index: "docs-v1_2.0"}, ok: true},
		{name: "maximum index", cfg: opensearch.Config{URL: "http://localhost", Index: strings.Repeat("a", 255)}, ok: true},
		{name: "empty URL", cfg: opensearch.Config{Index: "docs"}},
		{name: "missing host", cfg: opensearch.Config{URL: "http:///", Index: "docs"}},
		{name: "scheme", cfg: opensearch.Config{URL: "ftp://localhost", Index: "docs"}},
		{name: "credentials", cfg: opensearch.Config{URL: "http://private:secret@localhost", Index: "docs"}},
		{name: "path", cfg: opensearch.Config{URL: "http://localhost/private", Index: "docs"}},
		{name: "escaped root", cfg: opensearch.Config{URL: "http://localhost/%2f", Index: "docs"}},
		{name: "query", cfg: opensearch.Config{URL: "http://localhost?secret", Index: "docs"}},
		{name: "empty query", cfg: opensearch.Config{URL: "http://localhost?", Index: "docs"}},
		{name: "fragment", cfg: opensearch.Config{URL: "http://localhost#secret", Index: "docs"}},
		{name: "empty fragment", cfg: opensearch.Config{URL: "http://localhost#", Index: "docs"}},
		{name: "empty index", cfg: opensearch.Config{URL: "http://localhost"}},
		{name: "uppercase", cfg: opensearch.Config{URL: "http://localhost", Index: "Docs"}},
		{name: "too long", cfg: opensearch.Config{URL: "http://localhost", Index: strings.Repeat("a", 256)}},
		{name: "wildcard", cfg: opensearch.Config{URL: "http://localhost", Index: "docs*"}},
		{name: "multiple indexes", cfg: opensearch.Config{URL: "http://localhost", Index: "docs,other"}},
		{name: "path injection", cfg: opensearch.Config{URL: "http://localhost", Index: "docs/_search"}},
		{name: "escaped path", cfg: opensearch.Config{URL: "http://localhost", Index: "docs%2f_search"}},
		{name: "reserved prefix", cfg: opensearch.Config{URL: "http://localhost", Index: "_all"}},
		{name: "parent path", cfg: opensearch.Config{URL: "http://localhost", Index: ".."}},
		{name: "invalid username", cfg: opensearch.Config{URL: "http://localhost", Index: "docs", Username: "private:secret"}},
		{name: "password without username", cfg: opensearch.Config{URL: "http://localhost", Index: "docs", Password: "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := opensearch.New(tt.cfg)
			if tt.ok {
				require.NoError(t, err)
				require.NotNil(t, client)
				return
			}
			require.Error(t, err)
			require.Nil(t, client)
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), "private")
		})
	}
}

const mapping = `{"docs":{"mappings":{"dynamic":"strict","properties":{"organization_id":{"type":"keyword"},"document_id":{"type":"keyword"},"text":{"type":"text"}}}}}`
const exists = `{"error":{"type":"resource_already_exists_exception","reason":"private content"},"status":400}`

func TestClient_EnsureIndex(t *testing.T) {
	t.Parallel()
	var puts, gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "secret" {
			t.Error("missing basic authentication")
		}
		switch r.Method + " " + r.URL.Path {
		case "PUT /docs":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("missing JSON content type")
			}
			if string(body) != `{"mappings":{"dynamic":"strict","properties":{"organization_id":{"type":"keyword"},"document_id":{"type":"keyword"},"text":{"type":"text"}}}}` {
				t.Errorf("unexpected mapping: %s", body)
			}
			if puts.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"acknowledged":true,"shards_acknowledged":true,"index":"docs"}`)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, exists)
		case "GET /docs/_mapping":
			gets.Add(1)
			_, _ = io.WriteString(w, mapping)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "docs", Username: "user", Password: "secret"})
	require.NoError(t, err)
	require.NoError(t, client.EnsureIndex(t.Context()))
	require.NoError(t, client.EnsureIndex(t.Context()))
	require.EqualValues(t, 2, puts.Load())
	require.EqualValues(t, 1, gets.Load())
}

func TestClient_EnsureIndexFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		body      string
		mapping   string
		getStatus int
	}{
		{name: "unauthorized", status: 401, body: `private secret document content`},
		{name: "other conflict", status: 400, body: `{"error":{"type":"private_exception","reason":"secret"}}`},
		{name: "wrong conflict status", status: 500, body: exists},
		{name: "unacknowledged", status: 200, body: `{"acknowledged":false}`},
		{name: "malformed creation response", status: 200, body: `private secret`},
		{name: "oversized response", status: 200, body: strings.Repeat("x", (1<<20)+1)},
		{name: "wrong organization type", status: 400, body: exists, mapping: strings.Replace(mapping, `"organization_id":{"type":"keyword"}`, `"organization_id":{"type":"text"}`, 1)},
		{name: "wrong text type", status: 400, body: exists, mapping: strings.Replace(mapping, `"text":{"type":"text"}`, `"text":{"type":"keyword"}`, 1)},
		{name: "dynamic mapping", status: 400, body: exists, mapping: strings.Replace(mapping, `"strict"`, `"true"`, 1)},
		{name: "wrong index", status: 400, body: exists, mapping: strings.Replace(mapping, `"docs"`, `"other"`, 1)},
		{name: "missing mapping", status: 400, body: exists, mapping: `{}`},
		{name: "malformed mapping", status: 400, body: exists, mapping: `private secret`},
		{name: "mapping unauthorized", status: 400, body: exists, mapping: `private secret`, getStatus: 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("unexpected authentication")
				}
				if r.Method == http.MethodGet {
					if tt.getStatus != 0 {
						w.WriteHeader(tt.getStatus)
					}
					_, _ = io.WriteString(w, tt.mapping)
					return
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)
			client, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "docs"})
			require.NoError(t, err)
			err = client.EnsureIndex(t.Context())
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), server.URL)
		})
	}
}

func TestClient_Cancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "docs"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.EnsureIndex(ctx) }()
	<-started
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
}

func TestClient_RejectsRedirects(t *testing.T) {
	t.Parallel()
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
	}))
	t.Cleanup(target.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	client, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "docs", Username: "user", Password: "secret"})
	require.NoError(t, err)
	err = client.EnsureIndex(t.Context())
	require.ErrorContains(t, err, "307")
	require.False(t, redirected.Load())
	require.NotContains(t, err.Error(), server.URL)
}

func TestClient_TransportErrorIsPrivate(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	client, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "private-docs", Username: "user", Password: "secret"})
	require.NoError(t, err)
	err = client.EnsureIndex(t.Context())
	require.Error(t, err)
	require.NotContains(t, err.Error(), server.URL)
	require.NotContains(t, err.Error(), "private")
	require.NotContains(t, err.Error(), "secret")
}
