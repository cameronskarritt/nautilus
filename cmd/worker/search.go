package main

import (
	"context"

	"nautilus/internal/config"
	"nautilus/internal/embedding/lmstudio"
	"nautilus/internal/search"
	"nautilus/internal/search/hybrid"
	"nautilus/internal/search/opensearch"
)

func newIndexer(ctx context.Context) (search.Indexer, error) {
	cfg := opensearch.Config{
		URL:      config.Get("OPENSEARCH_URL", "http://localhost:9200"),
		Index:    config.Get("OPENSEARCH_INDEX", "nautilus-documents-v1"),
		Username: config.Get[string]("OPENSEARCH_USERNAME"),
		Password: config.Get[string]("OPENSEARCH_PASSWORD"),
	}
	endpoint := config.Get[string]("EMBEDDING_URL")
	if endpoint == "" {
		client, err := opensearch.New(cfg)
		if err != nil {
			return nil, err
		}
		if err := client.EnsureIndex(ctx); err != nil {
			return nil, err
		}
		return client, nil
	}
	embedder, err := lmstudio.New(lmstudio.Config{
		URL:              endpoint,
		Model:            config.Get("EMBEDDING_MODEL", "text-embedding-qwen3-embedding-4b"),
		Dimensions:       config.Get("EMBEDDING_DIMENSIONS", 2560),
		APIKey:           config.Get[string]("EMBEDDING_API_KEY"),
		QueryInstruction: config.Get[string]("EMBEDDING_QUERY_INSTRUCTION"),
	})
	if err != nil {
		return nil, err
	}
	cfg.Index = config.Get("OPENSEARCH_VECTOR_INDEX", "nautilus-documents-qwen3-4b-v1")
	store, err := opensearch.NewVector(cfg, embedder.Model())
	if err != nil {
		return nil, err
	}
	if err := store.EnsureIndex(ctx); err != nil {
		return nil, err
	}
	return hybrid.New(store, embedder, nil)
}
