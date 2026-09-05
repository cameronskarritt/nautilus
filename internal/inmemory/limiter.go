package inmemory

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"
)

type IdentityFunc func(ctx context.Context, r *http.Request) (string, error)

type MemoryLimiter struct {
	capacity   int
	interval   time.Duration
	leakRate   float64
	identifier IdentityFunc
	buckets    map[string]*bucket
	mu         sync.RWMutex
	timeFunc   func() time.Time
}

type bucket struct {
	level  float64
	lastTs time.Time
}

type MemoryLimiterConfig struct {
	Capacity     int
	Interval     time.Duration
	IdentityFunc IdentityFunc
}

func NewMemoryLimiter(ctx context.Context, config *MemoryLimiterConfig) *MemoryLimiter {
	if config.IdentityFunc == nil {
		panic("identity func must be set")
	}

	leakRate := float64(config.Capacity) / config.Interval.Seconds()

	limiter := &MemoryLimiter{
		capacity:   config.Capacity,
		interval:   config.Interval,
		leakRate:   leakRate,
		identifier: config.IdentityFunc,
		buckets:    make(map[string]*bucket),
		timeFunc:   time.Now,
	}

	go limiter.cleanup(ctx, time.Minute)
	return limiter
}

func (l *MemoryLimiter) Count(_ context.Context, key string) (int, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.timeFunc()
	b, exists := l.buckets[key]
	if !exists {
		b = &bucket{
			level:  0,
			lastTs: now,
		}
		l.buckets[key] = b
	}

	// Drain leaked amount based on elapsed time
	elapsed := now.Sub(b.lastTs).Seconds()
	b.level = math.Max(0, b.level-(elapsed*l.leakRate))
	b.lastTs = now

	// Check if request is allowed (level < capacity)
	allowed := b.level < float64(l.capacity)
	if allowed {
		b.level++
	}

	// Calculate retry_after if not allowed
	var retryAfter time.Duration
	if !allowed {
		// Time until one slot available: (level - capacity + 1) / leakRate
		retrySeconds := (b.level - float64(l.capacity) + 1) / l.leakRate
		retryAfter = time.Duration(retrySeconds * float64(time.Second))
	}

	return int(b.level), retryAfter, nil
}

func (l *MemoryLimiter) Limit(_ context.Context, _ string) (int, error) {
	return l.capacity, nil
}

func (l *MemoryLimiter) Identify(ctx context.Context, r *http.Request) (string, error) {
	return l.identifier(ctx, r)
}

func (l *MemoryLimiter) cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanupOldBuckets()
		}
	}
}

func (l *MemoryLimiter) cleanupOldBuckets() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.timeFunc()
	cutoff := now.Add(-l.interval * 2)

	for key, b := range l.buckets {
		if b.lastTs.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}
