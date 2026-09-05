package redis

import (
	"context"
	"time"

	"nautilus/internal/errors"
)

type CounterConfig struct {
	Interval time.Duration
}

type Counter struct {
	rdb      *Redis
	interval time.Duration
}

func NewCounter(_ context.Context, rdb *Redis, config *CounterConfig) *Counter {
	if config == nil {
		config = &CounterConfig{}
	}

	return &Counter{
		rdb:      rdb,
		interval: config.Interval,
	}
}

func (rc *Counter) Count(ctx context.Context, key string) (int, time.Duration, error) {
	client := rc.rdb.Client()

	count, err := client.Incr(ctx, key).Result()
	if err != nil {
		return -1, 0, errors.Wrap(err, "unable to increment counter")
	}

	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		return -1, 0, errors.Wrap(err, "unable to fetch ttl")
	}

	// Check for -1 here since we'll always set the key in INCR.
	if ttl == -1 {
		if rc.interval > 0 {
			if err := client.Expire(ctx, key, rc.interval).Err(); err != nil {
				return -1, 0, errors.Wrap(err, "unable to set expiration")
			}
			ttl = rc.interval
		}
	}

	return int(count), ttl, nil
}

func (rc *Counter) Expire(ctx context.Context, key string, at time.Duration) error {
	client := rc.rdb.Client()

	if at == -1 {
		if err := client.Del(ctx, key).Err(); err != nil {
			return errors.Wrap(err, "unable to delete key")
		}
		return nil
	}

	if err := client.Expire(ctx, key, at).Err(); err != nil {
		return errors.Wrap(err, "unable to set expiration")
	}

	return nil
}
