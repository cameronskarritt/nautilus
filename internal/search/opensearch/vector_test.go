package opensearch_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/embedding"
	"nautilus/internal/search"
	"nautilus/internal/search/opensearch"
	"nautilus/internal/testutil/require"
)

func TestVectorQueryContract(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/vectors/_search", r.URL.Path)
		require.Equal(t, "false", r.URL.Query().Get("allow_partial_search_results"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, float64(100), body["size"])
		require.Equal(t, []any{"organization_id", "document_id"}, body["_source"])
		boolQuery := body["query"].(map[string]any)["bool"].(map[string]any)
		require.Equal(t, map[string]any{"term": map[string]any{"organization_id": "org"}}, boolQuery["filter"])
		nested := boolQuery["must"].(map[string]any)["nested"].(map[string]any)
		require.Equal(t, map[string]any{"size": float64(1), "_source": []any{"chunks.text"}}, nested["inner_hits"])
		knn := nested["query"].(map[string]any)["knn"].(map[string]any)["chunks.vector"].(map[string]any)
		require.Equal(t, float64(100), knn["k"])
		require.Equal(t, boolQuery["filter"], knn["filter"])
		_, err := w.Write([]byte(`{"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"hits":[{"_source":{"organization_id":"org","document_id":"doc"},"inner_hits":{"chunks":{"hits":{"hits":[{"_source":{"text":"best chunk"}}]}}}}]}}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	client, err := opensearch.NewVector(opensearch.Config{URL: server.URL, Index: "vectors"}, embedding.Model{Name: "test", Dimensions: 3})
	require.NoError(t, err)
	docs, err := client.Nearest(t.Context(), "org", []float32{1, 0, 0}, &search.SearchOptions{Limit: 999})
	require.NoError(t, err)
	require.Equal(t, []search.Document{{ID: "doc", Text: "best chunk"}}, docs)
}

func TestVectorRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached OpenSearch") }))
	t.Cleanup(server.Close)
	client, err := opensearch.NewVector(opensearch.Config{URL: server.URL, Index: "vectors"}, embedding.Model{Name: "test", Dimensions: 3})
	require.NoError(t, err)
	tests := []struct {
		name  string
		chunk search.Chunk
	}{
		{"dimension", search.Chunk{Text: "text", Vector: []float32{1, 0}}},
		{"zero", search.Chunk{Text: "text", Vector: []float32{0, 0, 0}}},
		{"nan", search.Chunk{Text: "text", Vector: []float32{float32(math.NaN()), 0, 1}}},
		{"infinity", search.Chunk{Text: "text", Vector: []float32{float32(math.Inf(1)), 0, 1}}},
		{"oversized", search.Chunk{Text: strings.Repeat("a", 4097), Vector: []float32{1, 0, 0}}},
		{"invalid utf8", search.Chunk{Text: "\xff", Vector: []float32{1, 0, 0}}},
		{"empty", search.Chunk{Text: " ", Vector: []float32{1, 0, 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Error(t, client.Replace(t.Context(), "org", "doc", []search.Chunk{tt.chunk}))
		})
	}
	require.Error(t, client.Replace(t.Context(), "org", "doc", make([]search.Chunk, 129)))
	require.Error(t, client.Replace(t.Context(), "", "doc", nil))
	_, err = client.Nearest(t.Context(), "org", []float32{0, 0, 0}, nil)
	require.Error(t, err)
}

func TestVectorRejectsInvalidResults(t *testing.T) {
	t.Parallel()
	good := `{"_source":{"organization_id":"org","document_id":"doc"},"inner_hits":{"chunks":{"hits":{"hits":[{"_source":{"text":"best"}}]}}}}`
	tests := []struct {
		name     string
		hits     string
		timedOut bool
		failed   int
	}{
		{name: "tenant", hits: strings.Replace(good, `"org"`, `"other"`, 1)},
		{name: "duplicate", hits: good + "," + good},
		{name: "missing text", hits: strings.Replace(good, `"text":"best"`, `"other":"best"`, 1)},
		{name: "oversized text", hits: strings.Replace(good, "best", strings.Repeat("a", 4097), 1)},
		{name: "invalid utf8", hits: strings.Replace(good, "best", "\xff", 1)},
		{name: "timed out", hits: good, timedOut: true},
		{name: "failed shard", hits: good, failed: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := json.Marshal(map[string]any{"timed_out": tt.timedOut, "_shards": map[string]int{"total": 1, "successful": 1, "failed": tt.failed}})
				_, err := w.Write([]byte(strings.TrimSuffix(string(body), "}") + `,"hits":{"hits":[` + tt.hits + `]}}`))
				require.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			client, err := opensearch.NewVector(opensearch.Config{URL: server.URL, Index: "vectors"}, embedding.Model{Name: "test", Dimensions: 3})
			require.NoError(t, err)
			_, err = client.Keyword(t.Context(), "org", "query", nil)
			require.Error(t, err)
		})
	}
}

func TestVectorMappingCompatibility(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{name: "matching", change: func(map[string]any) {}},
		{name: "disabled knn", change: func(index map[string]any) {
			index["settings"].(map[string]any)["index"].(map[string]any)["knn"] = "false"
		}},
		{name: "changed model", change: func(index map[string]any) {
			index["mappings"].(map[string]any)["_meta"].(map[string]any)["model"] = "other"
		}},
		{name: "changed dimensions", change: func(index map[string]any) {
			index["mappings"].(map[string]any)["properties"].(map[string]any)["chunks"].(map[string]any)["properties"].(map[string]any)["vector"].(map[string]any)["dimension"] = 4
		}},
		{name: "keyword index", change: func(index map[string]any) {
			delete(index["mappings"].(map[string]any)["properties"].(map[string]any), "chunks")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var index map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					require.NoError(t, json.NewDecoder(r.Body).Decode(&index))
					index["settings"] = map[string]any{"index": map[string]any{"knn": "true"}}
					tt.change(index)
					w.WriteHeader(http.StatusBadRequest)
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": "resource_already_exists_exception"}}))
					return
				}
				require.Equal(t, http.MethodGet, r.Method)
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"vectors": index}))
			}))
			t.Cleanup(server.Close)
			client, err := opensearch.NewVector(opensearch.Config{URL: server.URL, Index: "vectors"}, embedding.Model{Name: "test", Dimensions: 3})
			require.NoError(t, err)
			err = client.EnsureIndex(t.Context())
			if tt.name == "matching" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestVectorEscapedChunkResponse(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("\x01", search.MaxChunkBytes)
	hits := make([]map[string]any, 100)
	for i := range hits {
		hits[i] = map[string]any{
			"_source":    map[string]string{"organization_id": "org", "document_id": strings.Repeat("x", i+1)},
			"inner_hits": map[string]any{"chunks": map[string]any{"hits": map[string]any{"hits": []any{map[string]any{"_source": map[string]string{"text": text}}}}}},
		}
	}
	body, err := json.Marshal(map[string]any{"timed_out": false, "_shards": map[string]int{"total": 1, "successful": 1, "failed": 0}, "hits": map[string]any{"hits": hits}})
	require.NoError(t, err)
	require.Greater(t, len(body), 1<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(server.Close)
	client, err := opensearch.NewVector(opensearch.Config{URL: server.URL, Index: "vectors"}, embedding.Model{Name: "test", Dimensions: 3})
	require.NoError(t, err)
	docs, err := client.Keyword(t.Context(), "org", "query", &search.SearchOptions{Limit: 100})
	require.NoError(t, err)
	require.Len(t, docs, 100)
	require.Equal(t, text, docs[99].Text)
	// The existing keyword client keeps its smaller response budget.
	keyword, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "vectors"})
	require.NoError(t, err)
	_, err = keyword.Search(t.Context(), "org", "query", &search.SearchOptions{Limit: 100})
	require.ErrorContains(t, err, "response exceeds size limit")
}

func TestVectorResponseLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat(" ", (4<<20)+1))) }))
	t.Cleanup(server.Close)
	client, err := opensearch.NewVector(opensearch.Config{URL: server.URL, Index: "vectors"}, embedding.Model{Name: "test", Dimensions: 3})
	require.NoError(t, err)
	_, err = client.Keyword(t.Context(), "org", "query", nil)
	require.ErrorContains(t, err, "response exceeds size limit")
}
