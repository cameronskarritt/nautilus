package hybrid_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"nautilus/internal/embedding"
	"nautilus/internal/errors"
	"nautilus/internal/search"
	"nautilus/internal/search/hybrid"
	"nautilus/internal/testutil/require"
)

func TestSearchFusion(t *testing.T) {
	t.Parallel()
	store := &testStore{
		keyword:  []search.Document{{ID: "a"}, {ID: "agreement"}, {ID: "b"}, {ID: "a"}},
		semantic: []search.Document{{ID: "z"}, {ID: "agreement"}, {ID: "c"}},
	}
	embedder := &testEmbedder{}
	client, err := hybrid.New(store, embedder, nil)
	require.NoError(t, err)
	ids, err := client.Search(t.Context(), "org", "payment deadline", nil)
	require.NoError(t, err)
	// Agreement across retrieval methods outranks either method's first result.
	// Duplicate entries cannot add votes, and equal scores have stable ID order.
	require.Equal(t, []string{"agreement", "a", "z", "b", "c"}, ids)
	require.Equal(t, []string{"org", "org"}, store.orgs)
	require.True(t, embedder.query)
	require.Equal(t, []string{"payment deadline"}, embedder.inputs)
	ids, err = client.Search(t.Context(), "org", "payment deadline", &search.SearchOptions{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"agreement", "a"}, ids)
}

func TestSearchReranker(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"reorder", "unknown ID", "duplicate ID", "missing ID", "provider failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &testStore{keyword: []search.Document{{ID: "a", Text: "first"}}, semantic: []search.Document{{ID: "b", Text: "second"}}}
			reranker := rankFunc(func(_ context.Context, query string, docs []search.Document) ([]string, error) {
				require.Equal(t, "query", query)
				require.Equal(t, []search.Document{{ID: "a", Text: "first"}, {ID: "b", Text: "second"}}, docs)
				switch name {
				case "unknown ID":
					return []string{"a", "foreign"}, nil
				case "duplicate ID":
					return []string{"a", "a"}, nil
				case "missing ID":
					return []string{"a"}, nil
				case "provider failure":
					return nil, errors.New("reranker unavailable")
				default:
					return []string{"b", "a"}, nil
				}
			})
			client, err := hybrid.New(store, &testEmbedder{}, reranker)
			require.NoError(t, err)
			ids, err := client.Search(t.Context(), "org", "query", &search.SearchOptions{Limit: 1})
			if name == "reorder" {
				require.NoError(t, err)
				require.Equal(t, []string{"b"}, ids)
			} else {
				require.Error(t, err)
				require.Nil(t, ids)
			}
		})
	}
}

func TestIndexChunks(t *testing.T) {
	t.Parallel()
	store, embedder := &testStore{}, &testEmbedder{}
	client, err := hybrid.New(store, embedder, nil)
	require.NoError(t, err)
	text := strings.Repeat("mail📬 invoice ", 7000)
	require.NoError(t, client.Index(t.Context(), "org", &search.Document{ID: "doc", Text: text}))
	require.Greater(t, embedder.calls, 1)
	require.Equal(t, "org", store.replacedOrg)
	require.Equal(t, "doc", store.replacedID)
	require.Greater(t, len(store.chunks), 16)
	rebuilt := store.chunks[0].Text
	for i, chunk := range store.chunks {
		require.True(t, utf8.ValidString(chunk.Text))
		require.LessOrEqual(t, len(chunk.Text), search.MaxChunkBytes)
		if i == 0 {
			continue
		}
		// The overlap stays close to 256 bytes without splitting UTF-8 runes.
		overlap := 256
		for overlap > 0 && !strings.HasSuffix(rebuilt, chunk.Text[:overlap]) {
			overlap--
		}
		require.GreaterOrEqual(t, overlap, 253)
		rebuilt += chunk.Text[overlap:]
	}
	require.Equal(t, text, rebuilt)
	// A shorter replacement contains none of the previous document's chunks.
	require.NoError(t, client.Index(t.Context(), "org", &search.Document{ID: "doc", Text: "replacement"}))
	require.Len(t, store.chunks, 1)
	require.Equal(t, "replacement", store.chunks[0].Text)
}

func TestIndexFailureDoesNotPublish(t *testing.T) {
	t.Parallel()
	store, embedder := &testStore{}, &testEmbedder{failAt: 2}
	client, err := hybrid.New(store, embedder, nil)
	require.NoError(t, err)
	err = client.Index(t.Context(), "org", &search.Document{ID: "doc", Text: strings.Repeat("x", 70000)})
	require.Error(t, err)
	require.Equal(t, 2, embedder.calls)
	require.Empty(t, store.chunks)
	require.Empty(t, store.replacedID)
}

func TestValidation(t *testing.T) {
	t.Parallel()
	store, embedder := &testStore{}, &testEmbedder{}
	_, err := hybrid.New(store, &testEmbedder{model: embedding.Model{Name: "different", Dimensions: 3}}, nil)
	require.Error(t, err)
	client, err := hybrid.New(store, embedder, nil)
	require.NoError(t, err)
	for _, doc := range []*search.Document{nil, {Text: "text"}, {ID: "doc"}, {ID: "doc", Text: "\xff"}, {ID: "doc", Text: strings.Repeat("x", search.MaxChunkBytes*search.MaxChunks)}} {
		require.Error(t, client.Index(t.Context(), "org", doc))
	}
	require.Zero(t, embedder.calls)
	_, err = client.Search(t.Context(), "", "query", nil)
	require.Error(t, err)
	_, err = client.Search(t.Context(), "org", strings.Repeat("x", 4097), nil)
	require.Error(t, err)
	ids, err := client.Search(t.Context(), "org", " \n", nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Search(ctx, "org", "query", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, store.orgs)
	require.Zero(t, embedder.calls)
	require.Error(t, client.Delete(t.Context(), "org", ""))
}

type testStore struct {
	keyword, semantic []search.Document
	orgs              []string
	replacedOrg       string
	replacedID        string
	chunks            []search.Chunk
}

func (*testStore) Model() embedding.Model { return embedding.Model{Name: "test", Dimensions: 3} }
func (s *testStore) Replace(_ context.Context, orgID, documentID string, chunks []search.Chunk) error {
	s.replacedOrg, s.replacedID, s.chunks = orgID, documentID, chunks
	return nil
}
func (*testStore) Delete(context.Context, string, string) error { return nil }
func (s *testStore) Keyword(_ context.Context, orgID, _ string, _ *search.SearchOptions) ([]search.Document, error) {
	s.orgs = append(s.orgs, orgID)
	return s.keyword, nil
}
func (s *testStore) Nearest(_ context.Context, orgID string, _ []float32, _ *search.SearchOptions) ([]search.Document, error) {
	s.orgs = append(s.orgs, orgID)
	return s.semantic, nil
}

type testEmbedder struct {
	model  embedding.Model
	inputs []string
	query  bool
	calls  int
	failAt int
}

func (e *testEmbedder) Model() embedding.Model {
	if e.model.Name != "" {
		return e.model
	}
	return embedding.Model{Name: "test", Dimensions: 3}
}
func (e *testEmbedder) Embed(_ context.Context, input []string, opts *embedding.Options) ([][]float32, error) {
	e.calls++
	if e.calls == e.failAt {
		return nil, errors.New("embedding unavailable")
	}
	e.inputs = append(e.inputs, input...)
	e.query = opts != nil && opts.Query
	if len(input) > 16 {
		return nil, errors.New("batch too large")
	}
	vectors := make([][]float32, len(input))
	for i := range vectors {
		vectors[i] = []float32{1, 0, 0}
	}
	return vectors, nil
}

type rankFunc func(context.Context, string, []search.Document) ([]string, error)

func (f rankFunc) Rerank(ctx context.Context, query string, docs []search.Document) ([]string, error) {
	return f(ctx, query, docs)
}
