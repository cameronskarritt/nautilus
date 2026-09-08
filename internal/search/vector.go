package search

import (
	"context"

	"nautilus/internal/embedding"
)

const (
	MaxChunkBytes = 4096
	MaxChunks     = 128
)

type Chunk struct {
	Text   string    `json:"text"`
	Vector []float32 `json:"vector"`
}

// VectorStore atomically replaces a document's chunks in one model's vector space.
// Results contain each document once, with its best matching bounded chunk text.
// Callers must recheck authoritative access and deletion state before use.
type VectorStore interface {
	Model() embedding.Model
	Replace(ctx context.Context, orgID, documentID string, chunks []Chunk) error
	Delete(ctx context.Context, orgID, documentID string) error
	Keyword(ctx context.Context, orgID, query string, opts *SearchOptions) ([]Document, error)
	Nearest(ctx context.Context, orgID string, vector []float32, opts *SearchOptions) ([]Document, error)
}
