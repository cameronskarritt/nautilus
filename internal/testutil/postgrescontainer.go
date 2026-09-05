package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"nautilus/internal/errors"
)

var (
	containerOnce sync.Once
	container     *postgres.PostgresContainer
	baseConnStr   string
	containerErr  error
)

// startContainer starts the postgres container once per test binary.
func startContainer() error {
	containerOnce.Do(func() {
		ctx := context.Background()

		container, containerErr = postgres.Run(ctx,
			"postgres:alpine",
			postgres.WithDatabase(testDBName),
			postgres.WithUsername(testUser),
			postgres.WithPassword(testPassword),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if containerErr != nil {
			return
		}

		baseConnStr, containerErr = container.ConnectionString(ctx, "sslmode=disable")
		if containerErr != nil {
			containerErr = errors.Wrap(containerErr, "error getting postgres connection string")
		}
	})

	return containerErr
}
