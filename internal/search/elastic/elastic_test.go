package elastic_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"nautilus/internal/optional"
	"nautilus/internal/search"
	"nautilus/internal/search/elastic"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type testDoc struct {
	Title string `json:"title"`
}

func setupStore(t *testing.T) *elastic.Store[testDoc] {
	t.Helper()
	client := testutil.ElasticsearchClient(t)
	index := "test-" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-")

	t.Cleanup(func() {
		client.Indices.Delete([]string{index})
	})

	return elastic.New[testDoc](client, index)
}

func TestStore_IndexAndSearch(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	ctx := context.Background()

	docs := []search.Document[testDoc]{
		{ID: "1", Content: "the quick brown fox jumps over the lazy dog", Data: testDoc{Title: "foxes"}},
		{ID: "2", Content: "a]lazy cat sleeps on the warm windowsill", Data: testDoc{Title: "cats"}},
		{ID: "3", Content: "the brown bear wanders through the forest", Data: testDoc{Title: "bears"}},
	}

	err := store.Index(ctx, docs)
	require.NoError(t, err)

	results, err := store.Search(ctx, "brown fox", nil)
	require.NoError(t, err)
	require.True(t, len(results) > 0)
	require.Equal(t, "1", results[0].ID)
	require.Equal(t, "foxes", results[0].Data.Title)
	require.True(t, results[0].Score > 0)
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	ctx := context.Background()

	docs := []search.Document[testDoc]{
		{ID: "1", Content: "golang is a programming language", Data: testDoc{Title: "go"}},
		{ID: "2", Content: "rust is a systems programming language", Data: testDoc{Title: "rust"}},
	}

	err := store.Index(ctx, docs)
	require.NoError(t, err)

	err = store.Delete(ctx, []string{"1"})
	require.NoError(t, err)

	results, err := store.Search(ctx, "programming language", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "2", results[0].ID)
}

func TestStore_SearchWithLimit(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	ctx := context.Background()

	docs := make([]search.Document[testDoc], 5)
	for i := range docs {
		docs[i] = search.Document[testDoc]{
			ID:      fmt.Sprintf("doc-%d", i),
			Content: "common search term across all documents",
			Data:    testDoc{Title: fmt.Sprintf("doc %d", i)},
		}
	}

	err := store.Index(ctx, docs)
	require.NoError(t, err)

	results, err := store.Search(ctx, "common search term", &search.SearchOptions{
		Limit: optional.Set(2),
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestStore_SearchKeywordMode(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	ctx := context.Background()

	docs := []search.Document[testDoc]{
		{ID: "1", Content: "elasticsearch is a search engine", Data: testDoc{Title: "es"}},
	}

	err := store.Index(ctx, docs)
	require.NoError(t, err)

	results, err := store.Search(ctx, "search engine", &search.SearchOptions{
		Mode: optional.Set(search.ModeKeyword),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "1", results[0].ID)
}

func TestStore_SearchSemanticModeReturnsError(t *testing.T) {
	t.Parallel()
	store := elastic.New[testDoc](nil, "")
	ctx := context.Background()

	_, err := store.Search(ctx, "anything", &search.SearchOptions{
		Mode: optional.Set(search.ModeSemantic),
	})
	require.ErrorIs(t, err, elastic.ErrSemanticNotSupported)
}

func TestStore_SearchEmptyResults(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	ctx := context.Background()

	docs := []search.Document[testDoc]{
		{ID: "1", Content: "the sun rises in the east", Data: testDoc{Title: "sun"}},
	}

	err := store.Index(ctx, docs)
	require.NoError(t, err)

	results, err := store.Search(ctx, "xyzzynonexistent", nil)
	require.NoError(t, err)
	require.Len(t, results, 0)
}

func TestStore_SearchEmptyIndex(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	ctx := context.Background()

	results, err := store.Search(ctx, "anything", nil)
	require.NoError(t, err)
	require.Nil(t, results)
}

func TestStore_EmptyInputs(t *testing.T) {
	t.Parallel()

	store := elastic.New[testDoc](nil, "")
	ctx := context.Background()

	require.NoError(t, store.Index(ctx, nil))
	require.NoError(t, store.Delete(ctx, nil))
}
