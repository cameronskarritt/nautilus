package testutil

import (
	"context"
	"sync"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/elasticsearch"

	"nautilus/internal/errors"
)

var (
	esContainerOnce sync.Once
	esContainer     *elasticsearch.ElasticsearchContainer
	esAddress       string
	esContainerErr  error
)

func startElasticsearchContainer() error {
	esContainerOnce.Do(func() {
		ctx := context.Background()

		esContainer, esContainerErr = elasticsearch.Run(ctx,
			"docker.elastic.co/elasticsearch/elasticsearch:8.17.0",
			testcontainers.WithEnv(map[string]string{
				"xpack.security.enabled": "false",
				"discovery.type":         "single-node",
				"ES_JAVA_OPTS":           "-Xms256m -Xmx256m",
			}),
		)
		if esContainerErr == nil {
			esAddress = esContainer.Settings.Address
		}
	})

	if esContainerErr != nil {
		return errors.Wrap(esContainerErr, "unable to start elasticsearch container")
	}
	return nil
}
