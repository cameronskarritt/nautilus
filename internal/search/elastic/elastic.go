package elastic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/elastic/go-elasticsearch/v8"

	"nautilus/internal/errors"
	"nautilus/internal/search"
)

var _ search.Index[any] = new(Store[any])

var ErrSemanticNotSupported = errors.New("semantic search not supported: no embedding provider configured")

const defaultLimit = 10

type Store[T any] struct {
	client *elasticsearch.Client
	index  string
}

func New[T any](client *elasticsearch.Client, index string) *Store[T] {
	return &Store[T]{
		client: client,
		index:  index,
	}
}

type document[T any] struct {
	Content string `json:"content"`
	Data    T      `json:"data"`
}

func (s *Store[T]) Index(ctx context.Context, docs []search.Document[T]) error {
	if len(docs) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, doc := range docs {
		meta := map[string]any{
			"index": map[string]any{
				"_index": s.index,
				"_id":    doc.ID,
			},
		}
		if err := json.NewEncoder(&buf).Encode(meta); err != nil {
			return errors.Wrapf(err, "elastic: failed to encode bulk meta for doc %s", doc.ID)
		}

		body := document[T]{
			Content: doc.Content,
			Data:    doc.Data,
		}
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return errors.Wrapf(err, "elastic: failed to encode bulk body for doc %s", doc.ID)
		}
	}

	res, err := s.client.Bulk(bytes.NewReader(buf.Bytes()),
		s.client.Bulk.WithContext(ctx),
		s.client.Bulk.WithRefresh("true"),
	)
	if err != nil {
		return errors.Wrap(err, "elastic: bulk index request failed")
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return errors.Errorf("elastic: bulk index returned %s: %s", res.Status(), body)
	}

	var bulkResp bulkResponse
	if err := json.NewDecoder(res.Body).Decode(&bulkResp); err != nil {
		return errors.Wrap(err, "elastic: failed to decode bulk response")
	}
	if bulkResp.Errors {
		for _, item := range bulkResp.Items {
			for _, action := range item {
				if action.Error != nil {
					return errors.Errorf("elastic: bulk index error on doc %s: %s — %s",
						action.ID, action.Error.Type, action.Error.Reason)
				}
			}
		}
	}

	return nil
}

func (s *Store[T]) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, id := range ids {
		meta := map[string]any{
			"delete": map[string]any{
				"_index": s.index,
				"_id":    id,
			},
		}
		if err := json.NewEncoder(&buf).Encode(meta); err != nil {
			return errors.Wrapf(err, "elastic: failed to encode delete meta for id %s", id)
		}
	}

	res, err := s.client.Bulk(bytes.NewReader(buf.Bytes()),
		s.client.Bulk.WithContext(ctx),
		s.client.Bulk.WithRefresh("true"),
	)
	if err != nil {
		return errors.Wrap(err, "elastic: bulk delete request failed")
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return errors.Errorf("elastic: bulk delete returned %s: %s", res.Status(), body)
	}

	return nil
}

func (s *Store[T]) Search(ctx context.Context, query string, opts *search.SearchOptions) ([]search.Result[T], error) {
	limit := defaultLimit
	mode := search.ModeHybrid

	if opts != nil {
		if opts.Limit.Set {
			limit = opts.Limit.Data
		}
		if opts.Mode.Set {
			mode = opts.Mode.Data
		}
	}

	if mode == search.ModeSemantic {
		return nil, ErrSemanticNotSupported
	}

	body := map[string]any{
		"size": limit,
		"query": map[string]any{
			"match": map[string]any{
				"content": query,
			},
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, errors.Wrap(err, "elastic: failed to encode search query")
	}

	res, err := s.client.Search(
		s.client.Search.WithContext(ctx),
		s.client.Search.WithIndex(s.index),
		s.client.Search.WithBody(&buf),
	)
	if err != nil {
		return nil, errors.Wrap(err, "elastic: search request failed")
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return nil, errors.Errorf("elastic: search returned %s: %s", res.Status(), body)
	}

	var searchResp searchResponse[T]
	if err := json.NewDecoder(res.Body).Decode(&searchResp); err != nil {
		return nil, errors.Wrap(err, "elastic: failed to decode search response")
	}

	results := make([]search.Result[T], 0, len(searchResp.Hits.Hits))
	for _, hit := range searchResp.Hits.Hits {
		results = append(results, search.Result[T]{
			Document: search.Document[T]{
				ID:      hit.ID,
				Content: hit.Source.Content,
				Data:    hit.Source.Data,
			},
			Score: hit.Score,
		})
	}

	return results, nil
}

type bulkResponse struct {
	Errors bool                              `json:"errors"`
	Items  []map[string]bulkResponseItemData `json:"items"`
}

type bulkResponseItemData struct {
	ID    string         `json:"_id"`
	Error *bulkItemError `json:"error,omitempty"`
}

type bulkItemError struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type searchResponse[T any] struct {
	Hits searchHits[T] `json:"hits"`
}

type searchHits[T any] struct {
	Hits []searchHit[T] `json:"hits"`
}

type searchHit[T any] struct {
	ID     string      `json:"_id"`
	Score  float64     `json:"_score"`
	Source document[T] `json:"_source"`
}
