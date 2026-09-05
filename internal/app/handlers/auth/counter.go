package auth

import (
	"context"
	"time"
)

type Counter interface {
	Count(ctx context.Context, key string) (int, time.Duration, error)
	Expire(ctx context.Context, key string, at time.Duration) error
}
