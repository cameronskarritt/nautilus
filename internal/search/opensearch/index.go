package opensearch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"nautilus/internal/errors"
	"nautilus/internal/search"
)

var _ search.Indexer = (*Client)(nil)

func (c *Client) Index(ctx context.Context, orgID string, doc *search.Document) error {
	if !search.ValidID(orgID) || doc == nil || !search.ValidID(doc.ID) {
		return errors.Wrap(search.ErrInvalidDocument, "OpenSearch indexing requires valid organization and document IDs")
	}
	// Allow a filename alongside the extraction service’s 100 MiB text limit.
	if len(doc.Text) > 101<<20 || !utf8.ValidString(doc.Text) {
		return errors.Wrap(search.ErrInvalidDocument, "OpenSearch document text must be valid UTF-8 and at most 101 MiB")
	}
	body, err := json.Marshal(struct {
		OrganizationID string `json:"organization_id"`
		DocumentID     string `json:"document_id"`
		Text           string `json:"text"`
	}{orgID, doc.ID, doc.Text})
	if err != nil {
		return errors.Wrap(err, "encode OpenSearch document")
	}
	id := documentID(orgID, doc.ID)
	status, body, err := c.request(ctx, http.MethodPut, "/_doc/"+id+"?refresh=wait_for", bytes.NewReader(body))
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return errors.Errorf("OpenSearch indexing returned HTTP %d", status)
	}
	var result writeResult
	if json.Unmarshal(body, &result) != nil || !result.valid(c.index, id) || (result.Result != "created" && result.Result != "updated") {
		return errors.New("OpenSearch indexing returned an invalid or incomplete result")
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, orgID, docID string) error {
	if !search.ValidID(orgID) || !search.ValidID(docID) {
		return errors.New("OpenSearch deletion requires valid organization and document IDs")
	}
	id := documentID(orgID, docID)
	status, body, err := c.request(ctx, http.MethodDelete, "/_doc/"+id+"?refresh=wait_for", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return errors.Errorf("OpenSearch deletion returned HTTP %d", status)
	}
	var result writeResult
	if json.Unmarshal(body, &result) != nil || !result.valid(c.index, id) || (status == http.StatusOK && result.Result != "deleted") || (status == http.StatusNotFound && result.Result != "not_found") {
		return errors.New("OpenSearch deletion returned an invalid or incomplete result")
	}
	return nil
}

func (c *Client) Search(ctx context.Context, orgID, query string, opts *search.SearchOptions) ([]string, error) {
	if !search.ValidID(orgID) {
		return nil, errors.New("OpenSearch search requires a valid organization ID")
	}
	if len(query) > 4<<10 || !utf8.ValidString(query) {
		return nil, errors.New("OpenSearch query must be valid UTF-8 and at most 4 KiB")
	}
	if strings.TrimSpace(query) == "" {
		return []string{}, nil
	}
	limit := 50
	if opts != nil && opts.Limit > 0 {
		limit = min(opts.Limit, 100)
	}
	body, err := json.Marshal(map[string]any{
		"size":    limit,
		"_source": []string{"organization_id", "document_id"},
		"query": map[string]any{
			"bool": map[string]any{
				"filter": map[string]any{"term": map[string]string{"organization_id": orgID}},
				"must":   map[string]any{"match": map[string]string{"text": query}},
			},
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "encode OpenSearch query")
	}
	status, body, err := c.request(ctx, http.MethodPost, "/_search?allow_partial_search_results=false", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errors.Errorf("OpenSearch search returned HTTP %d", status)
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
			} `json:"hits"`
		} `json:"hits"`
	}
	if json.Unmarshal(body, &result) != nil || result.TimedOut == nil || *result.TimedOut || result.Shards == nil || result.Shards.Total < 1 || result.Shards.Successful != result.Shards.Total || result.Shards.Failed == nil || *result.Shards.Failed != 0 || result.Hits == nil || result.Hits.Hits == nil || len(*result.Hits.Hits) > limit {
		return nil, errors.New("OpenSearch search returned an invalid or incomplete result")
	}
	ids := make([]string, 0, len(*result.Hits.Hits))
	seen := make(map[string]bool, len(*result.Hits.Hits))
	for _, hit := range *result.Hits.Hits {
		if hit.Source.OrganizationID != orgID || !search.ValidID(hit.Source.DocumentID) {
			return nil, errors.New("OpenSearch search returned an invalid document scope")
		}
		if !seen[hit.Source.DocumentID] {
			ids = append(ids, hit.Source.DocumentID)
			seen[hit.Source.DocumentID] = true
		}
	}
	return ids, nil
}

func documentID(orgID, docID string) string {
	// Prefix the organization length so distinct ID pairs cannot share a key.
	b := binary.BigEndian.AppendUint64(nil, uint64(len(orgID)))
	b = append(b, orgID...)
	b = append(b, docID...)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type writeResult struct {
	Index  string `json:"_index"`
	ID     string `json:"_id"`
	Result string `json:"result"`
	Shards *struct {
		Successful int  `json:"successful"`
		Failed     *int `json:"failed"`
	} `json:"_shards"`
}

func (r *writeResult) valid(index, id string) bool {
	return r.Index == index && r.ID == id && r.Shards != nil && r.Shards.Successful > 0 && r.Shards.Failed != nil && *r.Shards.Failed == 0
}
