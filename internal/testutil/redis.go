package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"nautilus/internal/errors"
)

var (
	redisContainerOnce sync.Once
	redisContainer     *redis.RedisContainer
	redisConnStr       string
	redisContainerErr  error
)

// startRedisContainer starts the redis container once per test binary.
func startRedisContainer() error {
	redisContainerOnce.Do(func() {
		ctx := context.Background()

		redisContainer, redisContainerErr = redis.Run(ctx,
			"redis:alpine",
			testcontainers.WithWaitStrategy(
				wait.ForLog("Ready to accept connections").
					WithStartupTimeout(30*time.Second),
			),
		)
		if redisContainerErr != nil {
			return
		}

		redisConnStr, redisContainerErr = redisContainer.ConnectionString(ctx)
		if redisContainerErr != nil {
			redisContainerErr = errors.Wrap(redisContainerErr, "error getting redis connection string")
		}
	})

	return redisContainerErr
}

// RedisConnString returns the connection string for a testcontainers Redis instance.
// The container is started once per test binary and reused across tests.
// Use this with redis.Connect to create connections in your tests.
func RedisConnString(t *testing.T) string {
	t.Helper()

	if err := startRedisContainer(); err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}

	return redisConnStr
}
