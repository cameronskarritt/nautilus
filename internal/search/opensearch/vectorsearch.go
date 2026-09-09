package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"nautilus/internal/errors"
	"nautilus/internal/search"
)

func (c *VectorClient) Keyword(ctx context.Context, orgID, query string, opts *search.SearchOptions) ([]search.Document, error) {
	if !search.ValidID(orgID) {
		return nil, errors.New("OpenSearch search requires a valid organization ID")
	}
	if len(query) > search.MaxChunkBytes || !utf8.ValidString(query) {
		return nil, errors.New("OpenSearch query must be valid UTF-8 and at most 4 KiB")
	}
	if strings.TrimSpace(query) == "" {
		return []search.Document{}, nil
	}
	return c.search(ctx, orgID, map[string]any{"match": map[string]string{"chunks.text": query}}, vectorLimit(opts))
}

func (c *VectorClient) Nearest(ctx context.Context, orgID string, vector []float32, opts *search.SearchOptions) ([]search.Document, error) {
	if !search.ValidID(orgID) {
		return nil, errors.New("OpenSearch search requires a valid organization ID")
	}
	if !c.validVector(vector) {
		return nil, errors.New("invalid OpenSearch query vector")
	}
	limit := vectorLimit(opts)
	// The inline parent filter restricts candidate selection before top-k, so a
	// neighboring organization's vectors cannot consume this organization's k.
	query := map[string]any{"knn": map[string]any{"chunks.vector": map[string]any{
		"vector": vector, "k": limit, "filter": map[string]any{"term": map[string]string{"organization_id": orgID}},
	}}}
	return c.search(ctx, orgID, query, limit)
}

func vectorLimit(opts *search.SearchOptions) int {
	if opts != nil && opts.Limit > 0 {
		return min(opts.Limit, 100)
	}
	return 50
}

func (c *VectorClient) search(ctx context.Context, orgID string, query map[string]any, limit int) ([]search.Document, error) {
	body, err := json.Marshal(map[string]any{
		"size": limit, "_source": []string{"organization_id", "document_id"},
		"query": map[string]any{"bool": map[string]any{
			"filter": map[string]any{"term": map[string]string{"organization_id": orgID}},
			"must": map[string]any{"nested": map[string]any{
				"path": "chunks", "score_mode": "max", "query": query,
				"inner_hits": map[string]any{"size": 1, "_source": []string{"chunks.text"}},
			}},
		}},
	})
	if err != nil {
		return nil, errors.Wrap(err, "encode OpenSearch vector query")
	}
	// JSON escaping can expand 100 bounded chunk texts beyond 1 MiB.
	status, body, err := c.client.requestLimit(ctx, http.MethodPost, "/_search?allow_partial_search_results=false", bytes.NewReader(body), 4<<20)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errors.Errorf("OpenSearch vector search returned HTTP %d", status)
	}
	var result struct {
		TimedOut *bool `json:"timed_out"`
		Shards   *struct {
			Total      int  `json:"total"`
			Successful int  `json:"successful"`
			Failed     *int `json:"failed"`
		} `json:"_shards"`
		Hits *struct {
			Hits *[]struct {
				Source struct {
					OrganizationID string `json:"organization_id"`
					DocumentID     string `json:"document_id"`
				} `json:"_source"`
				InnerHits map[string]struct {
					Hits struct {
						Hits []struct {
							Source struct {
								Text *string `json:"text"`
							} `json:"_source"`
						} `json:"hits"`
					} `json:"hits"`
				} `json:"inner_hits"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if !utf8.Valid(body) || json.Unmarshal(body, &result) != nil || result.TimedOut == nil || *result.TimedOut || result.Shards == nil || result.Shards.Total < 1 || result.Shards.Successful != result.Shards.Total || result.Shards.Failed == nil || *result.Shards.Failed != 0 || result.Hits == nil || result.Hits.Hits == nil || len(*result.Hits.Hits) > limit {
		return nil, errors.New("OpenSearch vector search returned an invalid or incomplete result")
	}
	docs := make([]search.Document, 0, len(*result.Hits.Hits))
	seen := make(map[string]bool, len(*result.Hits.Hits))
	for _, hit := range *result.Hits.Hits {
		chunks := hit.InnerHits["chunks"].Hits.Hits
		if hit.Source.OrganizationID != orgID || !search.ValidID(hit.Source.DocumentID) || seen[hit.Source.DocumentID] || len(chunks) != 1 || chunks[0].Source.Text == nil {
			return nil, errors.New("OpenSearch vector search returned an invalid document or chunk")
		}
		text := *chunks[0].Source.Text
		if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || len(text) > search.MaxChunkBytes {
			return nil, errors.New("OpenSearch vector search returned invalid chunk text")
		}
		docs = append(docs, search.Document{ID: hit.Source.DocumentID, Text: text})
		seen[hit.Source.DocumentID] = true
	}
	return docs, nil
}
