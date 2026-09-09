package hybrid

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nautilus/internal/embedding"
	"nautilus/internal/errors"
	"nautilus/internal/search"
)

var _ search.Indexer = (*Client)(nil)

type Client struct {
	store    search.VectorStore
	embedder embedding.Embedder
	reranker search.Reranker
}

// A nil reranker leaves the reciprocal rank fusion ordering intact.
func New(store search.VectorStore, embedder embedding.Embedder, reranker search.Reranker) (*Client, error) {
	if store == nil || embedder == nil {
		return nil, errors.New("document search requires a store and embedder")
	}
	model := embedder.Model()
	if model.Name == "" || model.Dimensions <= 0 || model != store.Model() {
		return nil, errors.New("document search embedding model does not match its index")
	}
	return &Client{store: store, embedder: embedder, reranker: reranker}, nil
}

func (c *Client) Index(ctx context.Context, orgID string, doc *search.Document) error {
	if !search.ValidID(orgID) || doc == nil || !search.ValidID(doc.ID) {
		return errors.Wrap(search.ErrInvalidDocument, "document indexing requires organization and document IDs")
	}
	texts, err := split(doc.Text)
	if err != nil {
		return err
	}
	chunks := make([]search.Chunk, 0, len(texts))
	for start := 0; start < len(texts); start += 16 {
		batch := texts[start:min(start+16, len(texts))]
		vectors, err := c.embedder.Embed(ctx, batch, nil)
		if err != nil {
			return err
		}
		if len(vectors) != len(batch) {
			return errors.New("document embedding response count does not match input")
		}
		for i, text := range batch {
			chunks = append(chunks, search.Chunk{Text: text, Vector: vectors[i]})
		}
	}
	// Replace only after every embedding succeeds, so a failed batch cannot
	// publish half a document or leave chunks from its previous version.
	return c.store.Replace(ctx, orgID, doc.ID, chunks)
}

func (c *Client) Delete(ctx context.Context, orgID, documentID string) error {
	if !search.ValidID(orgID) || !search.ValidID(documentID) {
		return errors.New("document deletion requires organization and document IDs")
	}
	return c.store.Delete(ctx, orgID, documentID)
}

func (c *Client) Search(ctx context.Context, orgID, query string, opts *search.SearchOptions) ([]string, error) {
	if !search.ValidID(orgID) || !utf8.ValidString(query) || len(query) > 4<<10 {
		return nil, errors.New("document search requires an organization ID and a UTF-8 query of at most 4 KiB")
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "document search interrupted")
	}
	if strings.TrimSpace(query) == "" {
		return []string{}, nil
	}
	limit := 50
	if opts != nil && opts.Limit > 0 {
		limit = min(opts.Limit, 100)
	}
	pool := min(100, max(50, 2*limit))
	options := &search.SearchOptions{Limit: pool}
	keyword, err := c.store.Keyword(ctx, orgID, query, options)
	if err != nil {
		return nil, err
	}
	if err := validateCandidates(keyword, pool); err != nil {
		return nil, err
	}
	semantic, err := c.semantic(ctx, orgID, query, options)
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "document search interrupted")
	}
	fallback := err != nil
	if fallback {
		semantic = nil
	}
	candidates := fuse(keyword, semantic)
	candidates = candidates[:min(pool, len(candidates))]
	ids := make([]string, len(candidates))
	for i, doc := range candidates {
		ids[i] = doc.ID
	}
	if !fallback && c.reranker != nil && len(candidates) > 0 {
		allowed := make(map[string]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
		ids, err = c.reranker.Rerank(ctx, query, candidates)
		if err != nil {
			return nil, err
		}
		if len(ids) != len(allowed) {
			return nil, errors.New("document reranker returned an incomplete ranking")
		}
		for _, id := range ids {
			if !allowed[id] {
				return nil, errors.New("document reranker returned an invalid ranking")
			}
			delete(allowed, id)
		}
	}
	return ids[:min(limit, len(ids))], nil
}

func (c *Client) semantic(ctx context.Context, orgID, query string, opts *search.SearchOptions) ([]search.Document, error) {
	// Semantic retrieval is optional; a stalled model must not hold keyword
	// results for the embedding client's longer indexing timeout.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	vectors, err := c.embedder.Embed(ctx, []string{query}, &embedding.Options{Query: true})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("query embedding response count does not match input")
	}
	documents, err := c.store.Nearest(ctx, orgID, vectors[0], opts)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "semantic retrieval interrupted")
	}
	return documents, validateCandidates(documents, opts.Limit)
}

func validateCandidates(documents []search.Document, limit int) error {
	if len(documents) > limit {
		return errors.New("document search returned too many candidates")
	}
	for _, doc := range documents {
		if !search.ValidID(doc.ID) || len(doc.Text) > search.MaxChunkBytes || !utf8.ValidString(doc.Text) {
			return errors.New("document search returned an invalid candidate")
		}
	}
	return nil
}

func fuse(lists ...[]search.Document) []search.Document {
	type candidate struct {
		doc   search.Document
		score float64
		rank  int
	}
	byID := make(map[string]*candidate)
	for _, list := range lists {
		seen := make(map[string]bool)
		rank := 0
		for _, doc := range list {
			if seen[doc.ID] {
				continue
			}
			seen[doc.ID] = true
			rank++
			item := byID[doc.ID]
			if item == nil {
				item = &candidate{doc: doc, rank: rank}
				byID[doc.ID] = item
			} else if rank < item.rank {
				item.doc, item.rank = doc, rank
			}
			item.score += 1 / (60 + float64(rank))
		}
	}
	items := make([]*candidate, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].doc.ID < items[j].doc.ID
		}
		return items[i].score > items[j].score
	})
	documents := make([]search.Document, len(items))
	for i, item := range items {
		documents[i] = item.doc
	}
	return documents
}

func split(text string) ([]string, error) {
	const overlap = 256
	const maxBytes = (search.MaxChunkBytes-overlap)*search.MaxChunks + overlap
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || len(text) > maxBytes {
		return nil, errors.Wrap(search.ErrInvalidDocument, "document text must be nonempty UTF-8 within the embedding chunk budget")
	}
	var chunks []string
	for start := 0; start < len(text); {
		end := min(start+search.MaxChunkBytes, len(text))
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		if strings.TrimSpace(text[start:end]) != "" {
			chunks = append(chunks, text[start:end])
		}
		if len(chunks) > search.MaxChunks {
			return nil, errors.Wrap(search.ErrInvalidDocument, "document text exceeds the embedding chunk budget")
		}
		if end == len(text) {
			break
		}
		start = end - overlap
		for !utf8.RuneStart(text[start]) {
			start++
		}
	}
	return chunks, nil
}
