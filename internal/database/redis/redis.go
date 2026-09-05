package redis

import (
	"context"

	"github.com/redis/go-redis/v9"

	"nautilus/internal/errors"
	"nautilus/internal/log"
)

type Redis struct {
	client *redis.Client
}

func (rdb *Redis) Close() error {
	err := rdb.client.Close()
	if err != nil {
		return errors.Wrap(err, "unable to close redis client")
	}
	return nil
}

func Connect(ctx context.Context, url string) (*Redis, error) {
	if url == "" {
		return nil, errors.New("REDIS_URL is required")
	}

	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.Wrap(err, "unable to parse redis opts")
	}
	client := redis.NewClient(opt)

	status := client.Ping(ctx)
	if err := status.Err(); err != nil {
		return nil, errors.Wrap(err, "unable to ping redis")
	}

	return &Redis{
		client: client,
	}, nil
}

func Close(ctx context.Context, rdb *Redis) func() {
	noop := func() {}
	if rdb == nil {
		return noop
	}

	return func() {
		logger := log.FromContext(ctx)
		logger.Debug("closing cache")
		err := rdb.Close()
		if err != nil {
			logger.Error("error closing cache", "error", err)
			return
		}
		logger.Debug("cache closed")
	}
}

func (rdb *Redis) Client() *redis.Client {
	return rdb.client
}
