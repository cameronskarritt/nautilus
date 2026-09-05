package testutil

import (
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
)

// ElasticsearchClient returns a configured Elasticsearch client connected to
// a testcontainers instance. The container is started once per test binary and
// reused across tests.
func ElasticsearchClient(t *testing.T) *elasticsearch.Client {
	t.Helper()

	if err := startElasticsearchContainer(); err != nil {
		t.Fatalf("failed to start elasticsearch container: %v", err)
	}

	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{esAddress},
	})
	if err != nil {
		t.Fatalf("failed to create elasticsearch client: %v", err)
	}

	res, err := client.Info()
	if err != nil {
		t.Fatalf("failed to ping elasticsearch: %v", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		t.Fatalf("elasticsearch info returned error: %s", res.Status())
	}

	return client
}
