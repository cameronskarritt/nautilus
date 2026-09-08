package hybrid_test

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestSearchKeywordFallback(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"embedding failure", "provider timeout", "missing embedding", "nearest failure", "invalid semantic candidate", "too many semantic candidates", "empty keywords"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &testStore{keyword: []search.Document{{ID: "z"}, {ID: "a"}, {ID: "z"}, {ID: "b"}}, semantic: []search.Document{{ID: "foreign"}}}
			embedder := &testEmbedder{}
			switch name {
			case "embedding failure", "empty keywords":
				embedder.failAt = 1
			case "provider timeout":
				embedder.embed = func(context.Context) ([][]float32, error) { return nil, context.DeadlineExceeded }
			case "missing embedding":
				embedder.embed = func(context.Context) ([][]float32, error) { return nil, nil }
			case "nearest failure":
				store.nearestErr = errors.New("semantic retrieval failed")
			case "invalid semantic candidate":
				store.semantic = []search.Document{{ID: ""}}
			case "too many semantic candidates":
				store.semantic = make([]search.Document, 51)
			}
			want := []string{"z", "a"}
			if name == "empty keywords" {
				store.keyword, want = nil, []string{}
			}
			reranker := rankFunc(func(context.Context, string, []search.Document) ([]string, error) {
				t.Fatal("keyword fallback must not call the reranker")
				return nil, nil
			})
			client, err := hybrid.New(store, embedder, reranker)
			require.NoError(t, err)
			ids, err := client.Search(t.Context(), "org", "query", &search.SearchOptions{Limit: 2})
			require.NoError(t, err)
			require.Equal(t, want, ids)
		})
	}
}

func TestSearchSemanticTimeout(t *testing.T) {
	t.Parallel()
	store := &testStore{keyword: []search.Document{{ID: "keyword"}}}
	embedder := &testEmbedder{embed: func(ctx context.Context) ([][]float32, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	client, err := hybrid.New(store, embedder, nil)
	require.NoError(t, err)
	ids, err := client.Search(t.Context(), "org", "query", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"keyword"}, ids)
	require.Equal(t, []string{"org"}, store.orgs)
}

func TestSearchCancellationDuringRetrieval(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"embedding", "nearest", "caller deadline"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store := &testStore{keyword: []search.Document{{ID: "keyword"}}}
			embedder := &testEmbedder{}
			want := context.Canceled
			switch stage {
			case "embedding":
				embedder.embed = func(context.Context) ([][]float32, error) {
					cancel()
					return nil, errors.New("provider interrupted")
				}
			case "nearest":
				store.nearest = cancel
			case "caller deadline":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
				want = context.DeadlineExceeded
				embedder.embed = func(ctx context.Context) ([][]float32, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
			}
			client, err := hybrid.New(store, embedder, nil)
			require.NoError(t, err)
			ids, err := client.Search(ctx, "org", "query", nil)
			require.ErrorIs(t, err, want)
			require.Nil(t, ids)
		})
	}
}

func TestSearchKeywordFailure(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"request failed", "invalid candidates"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &testStore{keywordErr: errors.New("keyword unavailable")}
			if name == "invalid candidates" {
				store.keywordErr, store.keyword = nil, []search.Document{{ID: ""}}
			}
			embedder := &testEmbedder{}
			client, err := hybrid.New(store, embedder, nil)
			require.NoError(t, err)
			ids, err := client.Search(t.Context(), "org", "query", nil)
			require.Error(t, err)
			require.Nil(t, ids)
			require.Zero(t, embedder.calls)
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
	keywordErr        error
	nearestErr        error
	nearest           func()
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
	return s.keyword, s.keywordErr
}
func (s *testStore) Nearest(_ context.Context, orgID string, _ []float32, _ *search.SearchOptions) ([]search.Document, error) {
	s.orgs = append(s.orgs, orgID)
	if s.nearest != nil {
		s.nearest()
	}
	return s.semantic, s.nearestErr
}

type testEmbedder struct {
	model  embedding.Model
	inputs []string
	query  bool
	calls  int
	failAt int
	embed  func(context.Context) ([][]float32, error)
}

func (e *testEmbedder) Model() embedding.Model {
	if e.model.Name != "" {
		return e.model
	}
	return embedding.Model{Name: "test", Dimensions: 3}
}
func (e *testEmbedder) Embed(ctx context.Context, input []string, opts *embedding.Options) ([][]float32, error) {
	e.calls++
	if e.embed != nil {
		return e.embed(ctx)
	}
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
