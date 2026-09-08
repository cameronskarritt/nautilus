package lmstudio

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"nautilus/internal/embedding"
	"nautilus/internal/errors"
)

const (
	MaxInputs          = 16
	MaxInputBytes      = 6000
	MaxBatchBytes      = 96 << 10
	maxResponseBytes   = 16 << 20
	defaultInstruction = "Given a document search query, retrieve relevant passages that answer the query."
)

type Config struct {
	// URL is the OpenAI-compatible base URL, such as http://localhost:1234/v1.
	URL              string
	Model            string
	Dimensions       int
	APIKey           string
	QueryInstruction string
}

type Client struct {
	endpoint    string
	model       embedding.Model
	apiKey      string
	instruction string
	http        *http.Client
}

var _ embedding.Embedder = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.URL, "#") {
		return nil, errors.New("invalid embedding base URL")
	}
	if strings.TrimSpace(cfg.Model) == "" || !utf8.ValidString(cfg.Model) {
		return nil, errors.New("embedding model is required")
	}
	if cfg.Dimensions < 1 || cfg.Dimensions > 16000 {
		return nil, errors.New("embedding dimensions must be between 1 and 16000")
	}
	if cfg.QueryInstruction == "" {
		cfg.QueryInstruction = defaultInstruction
	}
	if !utf8.ValidString(cfg.QueryInstruction) || strings.TrimSpace(cfg.QueryInstruction) == "" || len(cfg.QueryInstruction)+len("Instruct: \nQuery: ") >= MaxInputBytes {
		return nil, errors.New("invalid embedding query instruction")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/embeddings"
	return &Client{
		endpoint: u.String(), model: embedding.Model{Name: cfg.Model, Dimensions: cfg.Dimensions},
		apiKey: cfg.APIKey, instruction: cfg.QueryInstruction,
		http: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) Model() embedding.Model { return c.model }

func (c *Client) Embed(ctx context.Context, inputs []string, opts *embedding.Options) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "embedding request canceled")
	}
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	if len(inputs) > MaxInputs {
		return nil, errors.New("embedding batch exceeds input limit")
	}
	texts := make([]string, len(inputs))
	total := 0
	for i, input := range inputs {
		if len(input) > MaxInputBytes {
			return nil, errors.New("embedding input exceeds byte limit")
		}
		if !utf8.ValidString(input) || strings.TrimSpace(input) == "" {
			return nil, errors.New("embedding input must be nonblank UTF-8")
		}
		if opts != nil && opts.Query {
			input = "Instruct: " + c.instruction + "\nQuery: " + input
		}
		if len(input) > MaxInputBytes {
			return nil, errors.New("embedding input exceeds byte limit")
		}
		total += len(input)
		texts[i] = input
	}
	if total > MaxBatchBytes {
		return nil, errors.New("embedding batch exceeds byte limit")
	}
	body, err := json.Marshal(struct {
		Model    string   `json:"model"`
		Input    []string `json:"input"`
		Encoding string   `json:"encoding_format"`
	}{c.model.Name, texts, "float"})
	if err != nil {
		return nil, errors.New("encode embedding request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("create embedding request")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.Wrap(ctx.Err(), "embedding request canceled")
		}
		return nil, errors.New("embedding request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("embedding request returned HTTP %d", resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.Wrap(ctx.Err(), "embedding request canceled")
		}
		return nil, errors.New("read embedding response")
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("embedding response exceeds byte limit")
	}
	var result struct {
		Model string `json:"model"`
		Data  []struct {
			Index     *int       `json:"index"`
			Embedding []*float32 `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil, errors.New("invalid embedding response")
	}
	if result.Model != "" && result.Model != c.model.Name {
		return nil, errors.New("embedding response model mismatch")
	}
	if len(result.Data) != len(inputs) {
		return nil, errors.New("embedding response count mismatch")
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range result.Data {
		if item.Index == nil || *item.Index < 0 || *item.Index >= len(vectors) || vectors[*item.Index] != nil {
			return nil, errors.New("invalid embedding response index")
		}
		if len(item.Embedding) != c.model.Dimensions {
			return nil, errors.New("embedding response dimensions mismatch")
		}
		var norm float64
		for _, v := range item.Embedding {
			if v == nil || math.IsNaN(float64(*v)) || math.IsInf(float64(*v), 0) {
				return nil, errors.New("embedding response contains null or nonfinite coordinate")
			}
			norm += float64(*v) * float64(*v)
		}
		if norm == 0 {
			return nil, errors.New("embedding response contains zero vector")
		}
		norm = math.Sqrt(norm)
		vector := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			vector[i] = float32(float64(*v) / norm)
		}
		vectors[*item.Index] = vector
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "embedding request canceled")
	}
	return vectors, nil
}
