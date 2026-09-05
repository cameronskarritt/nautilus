package inmemory

import (
	"context"
	"sync"
	"time"
)

type MemoryCounterConfig struct {
	Interval time.Duration
}

func NewMemoryCounter(ctx context.Context, config *MemoryCounterConfig) *MemoryCounter {
	if config == nil {
		config = new(MemoryCounterConfig)
	}
	mc := &MemoryCounter{
		interval: config.Interval,
		counters: make(map[string]counter),
		timeFunc: time.Now,
	}
	go mc.pruneExpired(ctx, time.Minute)

	return mc
}

type counter struct {
	count    int
	expireAt time.Time
}

type MemoryCounter struct {
	interval time.Duration
	counters map[string]counter
	mu       sync.Mutex
	timeFunc func() time.Time
}

func (m *MemoryCounter) Count(_ context.Context, key string) (int, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.timeFunc()
	c, exists := m.counters[key]
	if !exists || (!c.expireAt.IsZero() && c.expireAt.Before(now)) {
		var exp time.Time
		if m.interval != 0 {
			exp = now.Add(m.interval)
		}
		c = counter{
			count:    0,
			expireAt: exp,
		}
	}
	c.count++
	m.counters[key] = c

	var remaining time.Duration
	if !c.expireAt.IsZero() {
		remaining = c.expireAt.Sub(now)
	}
	return c.count, remaining, nil
}

func (m *MemoryCounter) Expire(_ context.Context, key string, at time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if at == -1 {
		delete(m.counters, key)
		return nil
	}

	now := m.timeFunc()
	c, exists := m.counters[key]
	if exists && (c.expireAt.IsZero() || c.expireAt.After(now)) {
		c.expireAt = now.Add(at)
		m.counters[key] = c
	}

	return nil
}

func (m *MemoryCounter) pruneExpired(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.deleteExpiredKeys()
		}
	}
}

func (m *MemoryCounter) deleteExpiredKeys() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.timeFunc()
	for key, value := range m.counters {
		if !value.expireAt.IsZero() && value.expireAt.Before(now) {
			delete(m.counters, key)
		}
	}
}
