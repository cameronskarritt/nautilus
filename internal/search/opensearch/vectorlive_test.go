package opensearch_test

import (
	"context"
	"crypto/rand"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"nautilus/internal/embedding"
	"nautilus/internal/search"
	"nautilus/internal/search/opensearch"
	"nautilus/internal/testutil/require"
)

func TestLiveVectorStore(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_URL")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_URL is not configured")
	}
	t.Parallel()
	cfg := opensearch.Config{URL: endpoint, Index: "nautilus-vector-test-" + strings.ToLower(rand.Text()), Username: os.Getenv("OPENSEARCH_TEST_USERNAME"), Password: os.Getenv("OPENSEARCH_TEST_PASSWORD")}
	model := embedding.Model{Name: "synthetic-v1", Dimensions: 3}
	client, err := opensearch.NewVector(cfg, model)
	require.NoError(t, err)
	require.Error(t, client.Delete(t.Context(), "a", "absent"))
	require.NoError(t, client.EnsureIndex(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimSuffix(endpoint, "/")+"/"+cfg.Index, nil)
		require.NoError(t, err)
		if cfg.Username != "" {
			req.SetBasicAuth(cfg.Username, cfg.Password)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
	require.NoError(t, client.EnsureIndex(t.Context()))
	for _, wrong := range []embedding.Model{{Name: "other", Dimensions: 3}, {Name: model.Name, Dimensions: 4}} {
		other, err := opensearch.NewVector(cfg, wrong)
		require.NoError(t, err)
		require.Error(t, other.EnsureIndex(t.Context()))
	}
	require.NoError(t, client.Replace(t.Context(), "a", "same", []search.Chunk{{Text: "shared original", Vector: []float32{1, 0, 0}}, {Text: "stale token", Vector: []float32{0, 1, 0}}}))
	require.NoError(t, client.Replace(t.Context(), "a", "second", []search.Chunk{{Text: "shared second", Vector: []float32{0.8, 0.2, 0}}}))
	require.NoError(t, client.Replace(t.Context(), "b", "same", []search.Chunk{{Text: "shared private", Vector: []float32{1, 0, 0}}}))
	docs, err := client.Nearest(t.Context(), "a", []float32{1, 0, 0}, &search.SearchOptions{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []search.Document{{ID: "same", Text: "shared original"}, {ID: "second", Text: "shared second"}}, docs)
	docs, err = client.Keyword(t.Context(), "a", "shared", nil)
	require.NoError(t, err)
	require.Len(t, docs, 2)
	require.NoError(t, client.Replace(t.Context(), "a", "same", []search.Chunk{{Text: "replacement", Vector: []float32{0, 0, 1}}}))
	docs, err = client.Keyword(t.Context(), "a", "original stale", nil)
	require.NoError(t, err)
	require.Empty(t, docs)
	docs, err = client.Nearest(t.Context(), "a", []float32{1, 0, 0}, &search.SearchOptions{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []search.Document{{ID: "second", Text: "shared second"}}, docs)
	require.NoError(t, client.Delete(t.Context(), "a", "same"))
	require.NoError(t, client.Delete(t.Context(), "a", "same"))
	docs, err = client.Keyword(t.Context(), "b", "private", nil)
	require.NoError(t, err)
	require.Equal(t, []search.Document{{ID: "same", Text: "shared private"}}, docs)
	require.NoError(t, client.Replace(t.Context(), "a", "second", nil))
	docs, err = client.Nearest(t.Context(), "a", []float32{1, 0, 0}, nil)
	require.NoError(t, err)
	require.Empty(t, docs)
}
