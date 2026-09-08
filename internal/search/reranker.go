package search

import "context"

// Reranker reorders a bounded set of candidate passages for a query. Its result
// must contain every input document ID exactly once. It must not return document
// text, prompts, or provider response bodies in errors.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []Document) ([]string, error)
}
