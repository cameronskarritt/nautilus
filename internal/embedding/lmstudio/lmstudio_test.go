package lmstudio_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"nautilus/internal/embedding"
	"nautilus/internal/embedding/lmstudio"
	"nautilus/internal/testutil/require"
)

func TestNew(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(*lmstudio.Config)
	}{
		{"missing URL", func(c *lmstudio.Config) { c.URL = "" }},
		{"bad URL", func(c *lmstudio.Config) { c.URL = "%" }},
		{"scheme", func(c *lmstudio.Config) { c.URL = "file:///tmp/model" }},
		{"credentials", func(c *lmstudio.Config) { c.URL = "http://secret:password@localhost/v1" }},
		{"query", func(c *lmstudio.Config) { c.URL += "?key=secret" }},
		{"empty query", func(c *lmstudio.Config) { c.URL += "?" }},
		{"fragment", func(c *lmstudio.Config) { c.URL += "#secret" }},
		{"missing model", func(c *lmstudio.Config) { c.Model = "  " }},
		{"invalid model UTF8", func(c *lmstudio.Config) { c.Model = "\xff" }},
		{"zero dimensions", func(c *lmstudio.Config) { c.Dimensions = 0 }},
		{"negative dimensions", func(c *lmstudio.Config) { c.Dimensions = -1 }},
		{"excess dimensions", func(c *lmstudio.Config) { c.Dimensions = 16001 }},
		{"blank instruction", func(c *lmstudio.Config) { c.QueryInstruction = " " }},
		{"invalid instruction UTF8", func(c *lmstudio.Config) { c.QueryInstruction = "\xff" }},
		{"large instruction", func(c *lmstudio.Config) { c.QueryInstruction = strings.Repeat("x", 6000) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := lmstudio.Config{URL: "http://localhost:1234/v1", Model: "test", Dimensions: 2}
			tt.change(&cfg)
			_, err := lmstudio.New(cfg)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestClientEmbed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		opts        *embedding.Options
		instruction string
		want        string
	}{
		{name: "document", want: "first"},
		{name: "query", opts: &embedding.Options{Query: true}, want: "Instruct: Given a document search query, retrieve relevant passages that answer the query.\nQuery: first"},
		{name: "custom query", opts: &embedding.Options{Query: true}, instruction: "Find code.", want: "Instruct: Find code.\nQuery: first"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			type request struct{ method, path, auth, content, body string }
			requests := make(chan request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- request{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)}
				_, _ = io.WriteString(w, `{"model":"test","data":[{"index":1,"embedding":[0,2]},{"index":0,"embedding":[3,4]}]}`)
			}))
			t.Cleanup(server.Close)
			client, err := lmstudio.New(lmstudio.Config{URL: server.URL + "/proxy/v1/", Model: "test", Dimensions: 2, APIKey: "secret", QueryInstruction: tt.instruction})
			require.NoError(t, err)
			require.Equal(t, embedding.Model{Name: "test", Dimensions: 2}, client.Model())
			inputs := []string{"first", "second"}
			got, err := client.Embed(t.Context(), inputs, tt.opts)
			require.NoError(t, err)
			require.Equal(t, [][]float32{{0.6, 0.8}, {0, 1}}, got)
			require.Equal(t, []string{"first", "second"}, inputs)
			req := <-requests
			require.Equal(t, "POST", req.method)
			require.Equal(t, "/proxy/v1/embeddings", req.path)
			require.Equal(t, "Bearer secret", req.auth)
			require.Equal(t, "application/json", req.content)
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(req.body), &body))
			require.Len(t, body, 3)
			require.JSONEq(t, `"test"`, string(body["model"]))
			require.JSONEq(t, `"float"`, string(body["encoding_format"]))
			var texts []string
			require.NoError(t, json.Unmarshal(body["input"], &texts))
			require.Equal(t, tt.want, texts[0])
		})
	}
}

func TestClientEmbedInputs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		inputs  []string
		opts    *embedding.Options
		wantErr bool
	}{
		{name: "empty"},
		{name: "blank", inputs: []string{" \n"}, wantErr: true},
		{name: "UTF8", inputs: []string{"\xff"}, wantErr: true},
		{name: "too long", inputs: []string{strings.Repeat("x", lmstudio.MaxInputBytes+1)}, wantErr: true},
		{name: "query prefix limit", inputs: []string{strings.Repeat("x", lmstudio.MaxInputBytes)}, opts: &embedding.Options{Query: true}, wantErr: true},
		{name: "too many", inputs: make([]string, lmstudio.MaxInputs+1), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			t.Cleanup(server.Close)
			client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test", Dimensions: 2})
			require.NoError(t, err)
			got, err := client.Embed(t.Context(), tt.inputs, tt.opts)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Empty(t, got)
			}
			require.Zero(t, calls.Load())
		})
	}
}

func TestClientEmbedMaximumBatch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[`)
		for i := range lmstudio.MaxInputs {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = fmt.Fprintf(w, `{"index":%d,"embedding":[1]}`, i)
		}
		_, _ = io.WriteString(w, "]}")
	}))
	t.Cleanup(server.Close)
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test", Dimensions: 1})
	require.NoError(t, err)
	inputs := make([]string, lmstudio.MaxInputs)
	for i := range inputs {
		inputs[i] = strings.Repeat("x", lmstudio.MaxInputBytes)
	}
	got, err := client.Embed(t.Context(), inputs, nil)
	require.NoError(t, err)
	require.Len(t, got, lmstudio.MaxInputs)
}

func TestClientEmbedResponseErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body string
		status     int
	}{
		{name: "malformed", body: "secret response"},
		{name: "trailing JSON", body: `{"data":[]} {}`},
		{name: "missing", body: `{}`},
		{name: "missing index", body: `{"data":[{"embedding":[1,0]}]}`},
		{name: "negative index", body: `{"data":[{"index":-1,"embedding":[1,0]}]}`},
		{name: "out of range", body: `{"data":[{"index":1,"embedding":[1,0]}]}`},
		{name: "wrong dimensions", body: `{"data":[{"index":0,"embedding":[1]}]}`},
		{name: "null coordinate", body: `{"data":[{"index":0,"embedding":[null,1]}]}`},
		{name: "zero vector", body: `{"data":[{"index":0,"embedding":[0,0]}]}`},
		{name: "overflow", body: `{"data":[{"index":0,"embedding":[1e100,0]}]}`},
		{name: "NaN", body: `{"data":[{"index":0,"embedding":[NaN,0]}]}`},
		{name: "wrong model", body: `{"model":"secret","data":[{"index":0,"embedding":[1,0]}]}`},
		{name: "HTTP error", status: 500, body: "secret response"},
		{name: "redirect", status: 307, body: "secret response"},
		{name: "oversized", body: strings.Repeat(" ", (16<<20)+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tt.status != 0 {
					w.Header().Set("Location", "/secret")
					w.WriteHeader(tt.status)
				}
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)
			client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test", Dimensions: 2})
			require.NoError(t, err)
			got, err := client.Embed(t.Context(), []string{"secret input"}, nil)
			require.Error(t, err)
			require.Nil(t, got)
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), server.URL)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestClientEmbedDuplicateIndex(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[0,1]}]}`)
	}))
	t.Cleanup(server.Close)
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test", Dimensions: 2})
	require.NoError(t, err)
	_, err = client.Embed(t.Context(), []string{"first", "second"}, nil)
	require.Error(t, err)
}

func TestClientEmbedCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test", Dimensions: 2})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := client.Embed(ctx, []string{"first"}, nil); result <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	_, err = client.Embed(ctx, nil, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestClientEmbedLive(t *testing.T) {
	baseURL := os.Getenv("LMSTUDIO_TEST_URL")
	if baseURL == "" {
		t.Skip("set LMSTUDIO_TEST_URL to run the local model smoke test")
	}
	client, err := lmstudio.New(lmstudio.Config{URL: baseURL, Model: "text-embedding-qwen3-embedding-4b", Dimensions: 2560})
	require.NoError(t, err)
	docs, err := client.Embed(t.Context(), []string{"The capital of France is Paris.", "Bread dough rises when yeast ferments sugar."}, nil)
	require.NoError(t, err)
	queries, err := client.Embed(t.Context(), []string{"What is the capital of France?"}, &embedding.Options{Query: true})
	require.NoError(t, err)
	var relevant, unrelated float64
	for i, value := range queries[0] {
		relevant += float64(value) * float64(docs[0][i])
		unrelated += float64(value) * float64(docs[1][i])
	}
	require.Len(t, queries[0], 2560)
	require.Greater(t, relevant, unrelated)
	t.Logf("cosine: relevant %.4f, unrelated %.4f", relevant, unrelated)
}

func TestClientEmbedNetworkError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL + "/secret", Model: "test", Dimensions: 2, APIKey: "secret"})
	require.NoError(t, err)
	_, err = client.Embed(t.Context(), []string{"secret input"}, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	require.NotContains(t, err.Error(), server.URL)
}
