package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"nautilus/internal/embedding"
	"nautilus/internal/errors"
	"nautilus/internal/search"
)

type VectorClient struct {
	client *Client
	model  embedding.Model
}

var _ search.VectorStore = (*VectorClient)(nil)

func NewVector(cfg Config, model embedding.Model) (*VectorClient, error) {
	if strings.TrimSpace(model.Name) == "" || len(model.Name) > 512 || !utf8.ValidString(model.Name) || model.Dimensions < 1 || model.Dimensions > 16000 {
		return nil, errors.New("invalid OpenSearch embedding model")
	}
	client, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &VectorClient{client: client, model: model}, nil
}

func (c *VectorClient) Model() embedding.Model { return c.model }

func (c *VectorClient) EnsureIndex(ctx context.Context) error {
	mapping := map[string]any{
		"dynamic": "strict",
		"_meta":   map[string]any{"model": c.model.Name, "dimensions": c.model.Dimensions},
		"properties": map[string]any{
			"organization_id": map[string]any{"type": "keyword"},
			"document_id":     map[string]any{"type": "keyword"},
			"chunks": map[string]any{"type": "nested", "properties": map[string]any{
				"text":   map[string]any{"type": "text"},
				"vector": map[string]any{"type": "knn_vector", "dimension": c.model.Dimensions, "method": map[string]any{"name": "hnsw", "engine": "faiss", "space_type": "cosinesimil"}},
			}},
		},
	}
	body, err := json.Marshal(map[string]any{"settings": map[string]any{"index.knn": true}, "mappings": mapping})
	if err != nil {
		return errors.Wrap(err, "encode OpenSearch vector mapping")
	}
	status, body, err := c.client.request(ctx, http.MethodPut, "", bytes.NewReader(body))
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		var result struct {
			Acknowledged bool `json:"acknowledged"`
		}
		if json.Unmarshal(body, &result) != nil || !result.Acknowledged {
			return errors.New("OpenSearch index creation was not acknowledged")
		}
		return nil
	}
	var result struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if status != http.StatusBadRequest || json.Unmarshal(body, &result) != nil || result.Error.Type != "resource_already_exists_exception" {
		return errors.Errorf("OpenSearch vector index creation returned HTTP %d", status)
	}
	status, body, err = c.client.request(ctx, http.MethodGet, "", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errors.Errorf("OpenSearch vector index lookup returned HTTP %d", status)
	}
	var indexes map[string]struct {
		Mappings struct {
			Dynamic string `json:"dynamic"`
			Meta    struct {
				Model      string `json:"model"`
				Dimensions int    `json:"dimensions"`
			} `json:"_meta"`
			Properties map[string]vectorProperty `json:"properties"`
		} `json:"mappings"`
		Settings struct {
			Index struct {
				KNN string `json:"knn"`
			} `json:"index"`
		} `json:"settings"`
	}
	if json.Unmarshal(body, &indexes) != nil {
		return errors.New("invalid OpenSearch vector index response")
	}
	index := indexes[c.client.index]
	m := index.Mappings
	p := m.Properties
	chunk := p["chunks"]
	v := chunk.Properties["vector"]
	if len(indexes) != 1 || index.Settings.Index.KNN != "true" || m.Dynamic != "strict" || m.Meta.Model != c.model.Name || m.Meta.Dimensions != c.model.Dimensions || len(p) != 3 || p["organization_id"].Type != "keyword" || p["document_id"].Type != "keyword" || chunk.Type != "nested" || len(chunk.Properties) != 2 || chunk.Properties["text"].Type != "text" || v.Type != "knn_vector" || v.Dimension != c.model.Dimensions || v.Method.Name != "hnsw" || v.Method.Engine != "faiss" || v.Method.SpaceType != "cosinesimil" {
		return errors.New("OpenSearch vector index mapping or model is incompatible")
	}
	return nil
}

type vectorProperty struct {
	Type       string                    `json:"type"`
	Dimension  int                       `json:"dimension"`
	Properties map[string]vectorProperty `json:"properties"`
	Method     struct {
		Name      string `json:"name"`
		Engine    string `json:"engine"`
		SpaceType string `json:"space_type"`
	} `json:"method"`
}

func (c *VectorClient) Replace(ctx context.Context, orgID, docID string, chunks []search.Chunk) error {
	if !validID(orgID) || !validID(docID) {
		return errors.New("OpenSearch replacement requires valid organization and document IDs")
	}
	if len(chunks) > search.MaxChunks {
		return errors.New("OpenSearch document exceeds chunk limit")
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Text) == "" || len(chunk.Text) > search.MaxChunkBytes || !utf8.ValidString(chunk.Text) {
			return errors.New("OpenSearch chunk text must be nonempty valid UTF-8 and at most 4 KiB")
		}
		if !c.validVector(chunk.Vector) {
			return errors.New("invalid OpenSearch chunk vector")
		}
	}
	// Empty replacement removes all searchable state, including any prior chunks.
	if len(chunks) == 0 {
		return c.Delete(ctx, orgID, docID)
	}
	body, err := json.Marshal(struct {
		OrganizationID string         `json:"organization_id"`
		DocumentID     string         `json:"document_id"`
		Chunks         []search.Chunk `json:"chunks"`
	}{orgID, docID, chunks})
	if err != nil {
		return errors.Wrap(err, "encode OpenSearch chunks")
	}
	if len(body) > 64<<20 {
		return errors.New("OpenSearch document exceeds request size limit")
	}
	id := documentID(orgID, docID)
	status, body, err := c.client.request(ctx, http.MethodPut, "/_doc/"+id+"?refresh=wait_for", bytes.NewReader(body))
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return errors.Errorf("OpenSearch replacement returned HTTP %d", status)
	}
	var result writeResult
	if json.Unmarshal(body, &result) != nil || !result.valid(c.client.index, id) || (result.Result != "created" && result.Result != "updated") {
		return errors.New("OpenSearch replacement returned an invalid or incomplete result")
	}
	return nil
}

func (c *VectorClient) Delete(ctx context.Context, orgID, docID string) error {
	return c.client.Delete(ctx, orgID, docID)
}

func (c *VectorClient) validVector(v []float32) bool {
	if len(v) != c.model.Dimensions {
		return false
	}
	nonzero := false
	for _, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
		nonzero = nonzero || value != 0
	}
	return nonzero
}
