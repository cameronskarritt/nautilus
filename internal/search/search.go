package search

import (
	"context"

	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

var (
	ErrNotFound = errors.New("document not found")
)

// Index provides hybrid search (semantic + keyword) over a collection of documents.
type Index[T any] interface {
	Index(ctx context.Context, docs []Document[T]) error
	Delete(ctx context.Context, ids []string) error
	Search(ctx context.Context, query string, opts *SearchOptions) ([]Result[T], error)
}

type Document[T any] struct {
	ID      string
	Content string
	Data    T
}

type Result[T any] struct {
	Document[T]
	Score float64
}

type SearchOptions struct {
	Limit optional.Optional[int]
	Mode  optional.Optional[Mode]
}

type Mode string

const (
	ModeHybrid   Mode = "hybrid"
	ModeSemantic Mode = "semantic"
	ModeKeyword  Mode = "keyword"
)
