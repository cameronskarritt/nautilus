package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
)

type Ratelimiter interface {
	Count(ctx context.Context, key string) (int, time.Duration, error)
	Limit(ctx context.Context, key string) (int, error)
	Identify(ctx context.Context, r *http.Request) (string, error)
}

func Ratelimit(ratelimiter Ratelimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			id, err := ratelimiter.Identify(ctx, r)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}

			count, resetIn, err := ratelimiter.Count(ctx, id)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}

			limit, err := ratelimiter.Limit(ctx, id)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}

			timeRemaining := int(resetIn.Seconds())

			if count > limit {
				w.Header().Set("Retry-After", strconv.Itoa(timeRemaining))
				httputil.Error(ctx, w, errors.ErrTooManyRequests)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(limit-count))
			w.Header().Set("X-RateLimit-Reset", strconv.Itoa(timeRemaining))

			next.ServeHTTP(w, r)
		})
	}
}
