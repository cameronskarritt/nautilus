package opensearch_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync/atomic"
	"testing"

	"nautilus/internal/search"
	"nautilus/internal/search/opensearch"
	"nautilus/internal/testutil/require"
)

func newClient(t *testing.T, handler http.HandlerFunc) *opensearch.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := opensearch.New(opensearch.Config{URL: server.URL, Index: "docs"})
	require.NoError(t, err)
	return client
}

func writeResponse(t *testing.T, w http.ResponseWriter, r *http.Request, status int, result string) {
	t.Helper()
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"_index": "docs", "_id": path.Base(r.URL.Path), "result": result,
		"_shards": map[string]int{"total": 2, "successful": 1, "failed": 0},
	}); err != nil {
		t.Error(err)
	}
}

func TestClient_IndexAndDeleteScope(t *testing.T) {
	t.Parallel()
	type request struct {
		method string
		path   string
		body   map[string]string
	}
	requests := make(chan request, 10)
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("refresh") != "wait_for" {
			t.Error("write does not wait for search visibility")
		}
		var body map[string]string
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			writeResponse(t, w, r, http.StatusCreated, "created")
		} else {
			writeResponse(t, w, r, http.StatusOK, "deleted")
		}
		requests <- request{r.Method, r.URL.Path, body}
	})
	doc := &search.Document{ID: "same/id?", Text: "original text"}
	require.NoError(t, client.Index(t.Context(), "org-a", doc))
	first := <-requests
	require.Equal(t, http.MethodPut, first.method)
	require.Equal(t, map[string]string{"organization_id": "org-a", "document_id": doc.ID, "text": doc.Text}, first.body)
	require.Regexp(t, `^/docs/_doc/[a-f0-9]{64}$`, first.path)
	doc.Text = "replacement text"
	require.NoError(t, client.Index(t.Context(), "org-a", doc))
	replacement := <-requests
	require.Equal(t, first.path, replacement.path)
	require.Equal(t, "replacement text", replacement.body["text"])
	require.NoError(t, client.Index(t.Context(), "org-b", doc))
	other := <-requests
	require.NotEqual(t, first.path, other.path)
	require.NoError(t, client.Delete(t.Context(), "org-a", doc.ID))
	deleted := <-requests
	require.Equal(t, http.MethodDelete, deleted.method)
	require.Equal(t, first.path, deleted.path)

	require.NoError(t, client.Index(t.Context(), "a", &search.Document{ID: "bc"}))
	a := <-requests
	require.NoError(t, client.Index(t.Context(), "ab", &search.Document{ID: "c"}))
	b := <-requests
	require.NotEqual(t, a.path, b.path)
}

func TestClient_ValidationBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })
	tests := []struct {
		name string
		run  func() error
	}{
		{"index empty organization", func() error { return client.Index(t.Context(), " ", &search.Document{ID: "doc"}) }},
		{"index nil document", func() error { return client.Index(t.Context(), "org", nil) }},
		{"index empty document ID", func() error { return client.Index(t.Context(), "org", &search.Document{}) }},
		{"index oversized ID", func() error { return client.Index(t.Context(), "org", &search.Document{ID: strings.Repeat("a", 513)}) }},
		{"index oversized text", func() error {
			return client.Index(t.Context(), "org", &search.Document{ID: "doc", Text: strings.Repeat("x", (101<<20)+1)})
		}},
		{"index invalid UTF-8", func() error { return client.Index(t.Context(), "org", &search.Document{ID: "doc", Text: "\xff"}) }},
		{"delete empty organization", func() error { return client.Delete(t.Context(), "", "doc") }},
		{"delete oversized organization", func() error { return client.Delete(t.Context(), strings.Repeat("a", 513), "doc") }},
		{"delete empty document", func() error { return client.Delete(t.Context(), "org", "\n") }},
		{"delete invalid ID", func() error { return client.Delete(t.Context(), "org", "\xff") }},
		{"search empty organization", func() error { _, err := client.Search(t.Context(), " ", "", nil); return err }},
		{"search oversized query", func() error {
			_, err := client.Search(t.Context(), "org", strings.Repeat("a", (4<<10)+1), nil)
			return err
		}},
		{"search invalid UTF-8", func() error { _, err := client.Search(t.Context(), "org", "\xff", nil); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Error(t, tt.run()) })
	}
	ids, err := client.Search(t.Context(), "org", "\t \n", nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.Zero(t, requests.Load())
}

func TestClient_WriteResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		delete bool
		status int
		result string
		body   string
		ok     bool
	}{
		{name: "updated", status: 200, result: "updated", ok: true},
		{name: "absent document", delete: true, status: 404, result: "not_found", ok: true},
		{name: "absent index", delete: true, status: 404, body: `{"error":{"type":"index_not_found_exception","reason":"secret"},"status":404}`},
		{name: "failed index", status: 500, body: `private secret`},
		{name: "failed delete", delete: true, status: 503, body: `private secret`},
		{name: "unexpected index result", status: 200, result: "noop"},
		{name: "unexpected delete result", delete: true, status: 200, result: "not_found"},
		{name: "malformed JSON", status: 201, body: `private secret`},
		{name: "missing response", status: 200, body: `{}`},
		{name: "missing shards", status: 201, body: `{"result":"created"}`},
		{name: "partial index", status: 201, result: "created", body: "partial"},
		{name: "partial delete", delete: true, status: 200, result: "deleted", body: "partial"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.body == "partial" {
					w.WriteHeader(tt.status)
					if err := json.NewEncoder(w).Encode(map[string]any{
						"_index": "docs", "_id": path.Base(r.URL.Path), "result": tt.result,
						"_shards": map[string]int{"successful": 1, "failed": 1},
					}); err != nil {
						t.Error(err)
					}
					return
				}
				if tt.body != "" {
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, tt.body)
					return
				}
				writeResponse(t, w, r, tt.status, tt.result)
			})
			var err error
			if tt.delete {
				err = client.Delete(t.Context(), "org", "doc")
			} else {
				err = client.Index(t.Context(), "org", &search.Document{ID: "doc", Text: "private secret"})
			}
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

const searchResponse = `{"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"hits":[{"_source":{"organization_id":"org","document_id":"b"}},{"_source":{"organization_id":"org","document_id":"a"}},{"_source":{"organization_id":"org","document_id":"b"}}]}}`

func TestClient_Search(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		opts  *search.SearchOptions
		limit int
	}{
		{"default", nil, 50},
		{"nonpositive", &search.SearchOptions{Limit: -1}, 50},
		{"explicit", &search.SearchOptions{Limit: 3}, 3},
		{"bounded", &search.SearchOptions{Limit: 1000}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requests := make(chan map[string]any, 1)
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/docs/_search" || r.URL.Query().Get("allow_partial_search_results") != "false" {
					t.Error("unexpected search request")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests <- body
				_, _ = io.WriteString(w, searchResponse)
			})
			query := `hello "} OR organization_id:*`
			ids, err := client.Search(t.Context(), "org", query, tt.opts)
			require.NoError(t, err)
			require.Equal(t, []string{"b", "a"}, ids)
			body := <-requests
			require.EqualValues(t, tt.limit, body["size"])
			require.Equal(t, []any{"organization_id", "document_id"}, body["_source"])
			encoded, err := json.Marshal(body["query"])
			require.NoError(t, err)
			require.JSONEq(t, `{"bool":{"filter":{"term":{"organization_id":"org"}},"must":{"match":{"text":"hello \"} OR organization_id:*"}}}}`, string(encoded))
		})
	}
}

func TestClient_SearchFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"HTTP error", "private secret", 500},
		{"malformed JSON", "private secret", 200},
		{"missing structure", `{}`, 200},
		{"timeout", strings.Replace(searchResponse, `"timed_out":false`, `"timed_out":true`, 1), 200},
		{"missing timeout", strings.Replace(searchResponse, `"timed_out":false,`, ``, 1), 200},
		{"partial shards", strings.Replace(searchResponse, `"failed":0`, `"failed":1`, 1), 200},
		{"missing failed shards", strings.Replace(searchResponse, `,"failed":0`, ``, 1), 200},
		{"missing hits", strings.Replace(searchResponse, `"hits":{"hits":`, `"missing":{"hits":`, 1), 200},
		{"null hits", `{"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"hits":null}}`, 200},
		{"wrong organization", strings.Replace(searchResponse, `"organization_id":"org"`, `"organization_id":"other"`, 1), 200},
		{"missing source", strings.Replace(searchResponse, `"_source":`, `"missing":`, 1), 200},
		{"empty document ID", strings.Replace(searchResponse, `"document_id":"b"`, `"document_id":""`, 1), 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})
			ids, err := client.Search(t.Context(), "org", "private secret", nil)
			require.Error(t, err)
			require.Nil(t, ids)
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
