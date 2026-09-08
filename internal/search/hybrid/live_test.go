package hybrid_test

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"nautilus/internal/embedding/lmstudio"
	"nautilus/internal/search"
	"nautilus/internal/search/hybrid"
	"nautilus/internal/search/opensearch"
	"nautilus/internal/testutil/require"
)

func TestLiveHybridSearch(t *testing.T) {
	embeddingURL, endpoint := os.Getenv("LMSTUDIO_TEST_URL"), os.Getenv("OPENSEARCH_TEST_URL")
	if embeddingURL == "" || endpoint == "" {
		t.Skip("LMSTUDIO_TEST_URL and OPENSEARCH_TEST_URL are required")
	}
	embedder, err := lmstudio.New(lmstudio.Config{URL: embeddingURL, Model: "text-embedding-qwen3-embedding-4b", Dimensions: 2560})
	require.NoError(t, err)
	cfg := opensearch.Config{
		URL: endpoint, Index: "nautilus-hybrid-test-" + strings.ToLower(rand.Text()),
		Username: os.Getenv("OPENSEARCH_TEST_USERNAME"), Password: os.Getenv("OPENSEARCH_TEST_PASSWORD"),
	}
	store, err := opensearch.NewVector(cfg, embedder.Model())
	require.NoError(t, err)
	require.NoError(t, store.EnsureIndex(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimSuffix(endpoint, "/")+"/"+cfg.Index, nil)
		require.NoError(t, err)
		if cfg.Username != "" {
			req.SetBasicAuth(cfg.Username, cfg.Password)
		}
		client := &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
	client, err := hybrid.New(store, embedder, nil)
	require.NoError(t, err)
	require.NoError(t, client.Index(t.Context(), "org-a", &search.Document{ID: "capital", Text: "The capital of France is Paris."}))
	require.NoError(t, client.Index(t.Context(), "org-a", &search.Document{ID: "bread", Text: "Bread dough rises when yeast ferments sugar."}))
	require.NoError(t, client.Index(t.Context(), "org-b", &search.Document{ID: "foreign-capital", Text: "The capital of France is Paris."}))

	const query = "What is the capital of France?"
	ids, err := client.Search(t.Context(), "org-a", query, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"capital", "bread"}, ids)
	ids, err = client.Search(t.Context(), "org-b", query, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"foreign-capital"}, ids)

	require.NoError(t, client.Delete(t.Context(), "org-a", "capital"))
	ids, err = client.Search(t.Context(), "org-a", query, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"bread"}, ids)
	ids, err = client.Search(t.Context(), "org-b", query, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"foreign-capital"}, ids)
}
