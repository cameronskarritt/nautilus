package opensearch_test

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"nautilus/internal/search"
	"nautilus/internal/search/opensearch"
	"nautilus/internal/testutil/require"
)

func TestLiveIndexer(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_URL")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_URL is not configured")
	}
	t.Parallel()
	cfg := opensearch.Config{
		URL:      endpoint,
		Index:    "nautilus-test-" + strings.ToLower(rand.Text()),
		Username: os.Getenv("OPENSEARCH_TEST_USERNAME"),
		Password: os.Getenv("OPENSEARCH_TEST_PASSWORD"),
	}
	client, err := opensearch.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimSuffix(endpoint, "/")+"/"+cfg.Index, nil)
		require.NoError(t, err)
		if cfg.Username != "" {
			req.SetBasicAuth(cfg.Username, cfg.Password)
		}
		httpClient := &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
	require.NoError(t, client.EnsureIndex(t.Context()))
	require.NoError(t, client.EnsureIndex(t.Context()))
	require.NoError(t, client.Index(t.Context(), "org-a", &search.Document{ID: "same", Text: "shared original"}))
	require.NoError(t, client.Index(t.Context(), "org-b", &search.Document{ID: "same", Text: "shared private"}))
	require.NoError(t, client.Index(t.Context(), "org-a", &search.Document{ID: "second", Text: "shared second"}))
	ids, err := client.Search(t.Context(), "org-a", "shared", nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"same", "second"}, ids)
	ids, err = client.Search(t.Context(), "org-b", "shared", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"same"}, ids)
	ids, err = client.Search(t.Context(), "org-a", "private", nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = client.Search(t.Context(), "org-a", "shared", &search.SearchOptions{Limit: 1})
	require.NoError(t, err)
	require.Len(t, ids, 1)

	doc := &search.Document{ID: "same", Text: "shared replacement"}
	require.NoError(t, client.Index(t.Context(), "org-a", doc))
	require.NoError(t, client.Index(t.Context(), "org-a", doc))
	ids, err = client.Search(t.Context(), "org-a", "original", nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = client.Search(t.Context(), "org-a", "replacement", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"same"}, ids)
	require.NoError(t, client.Delete(t.Context(), "org-a", "same"))
	require.NoError(t, client.Delete(t.Context(), "org-a", "same"))
	ids, err = client.Search(t.Context(), "org-a", "shared", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"second"}, ids)
	ids, err = client.Search(t.Context(), "org-b", "private", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"same"}, ids)
}
