package search

import (
	"context"

	"nautilus/internal/errors"
)

// ErrInvalidDocument marks indexing input that cannot succeed unchanged.
var ErrInvalidDocument = errors.New("invalid document")

// Indexer manages searchable text within an organization. Implementations must
// reject an empty organization ID and scope every operation to that organization.
type Indexer interface {
	// Index replaces the text for doc.ID; retrying the same document is idempotent.
	// Invalid input returns an error wrapping ErrInvalidDocument.
	Index(ctx context.Context, orgID string, doc *Document) error
	// Delete succeeds if the document is already absent.
	Delete(ctx context.Context, orgID, documentID string) error
	// Search returns document IDs in relevance order, without duplicates.
	// Callers must recheck authoritative access and deletion state before use.
	Search(ctx context.Context, orgID, query string, opts *SearchOptions) ([]string, error)
}

type Document struct {
	ID   string
	Text string
}

type SearchOptions struct {
	// Limit caps the number of results. Nil options or a nonpositive limit use
	// the implementation's default.
	Limit int
}
