package inmemory

import (
	"context"
	"sync"
	"testing"
	"time"

	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestMemoryCounter_Count(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 8, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		Name     string
		Counters map[string]counter
		Interval time.Duration
		Key      string
		Count    int
		TTL      time.Duration
	}{
		{
			Name:     "new key with interval",
			Counters: map[string]counter{},
			Interval: time.Minute,
			Key:      "new",
			Count:    1,
			TTL:      time.Minute,
		},
		{
			Name:     "new key without interval",
			Counters: map[string]counter{},
			Key:      "new",
			Count:    1,
		},
		{
			Name: "existing active key",
			Counters: map[string]counter{
				"existing": {count: 5, expireAt: now.Add(30 * time.Second)},
			},
			Interval: time.Minute,
			Key:      "existing",
			Count:    6,
			TTL:      30 * time.Second,
		},
		{
			Name: "expired key resets",
			Counters: map[string]counter{
				"expired": {count: 10, expireAt: now.Add(-time.Second)},
			},
			Interval: time.Minute,
			Key:      "expired",
			Count:    1,
			TTL:      time.Minute,
		},
		{
			Name: "zero expiry stays unbounded",
			Counters: map[string]counter{
				"unbounded": {count: 3},
			},
			Interval: time.Minute,
			Key:      "unbounded",
			Count:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			mc := testCounter(now, tt.Interval, tt.Counters)
			count, ttl, err := mc.Count(context.Background(), tt.Key)

			require.NoError(t, err)
			require.Equal(t, tt.Count, count)
			require.Equal(t, tt.TTL, ttl)
		})
	}
}

func TestMemoryCounter_Expire(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 8, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		Name     string
		Counters map[string]counter
		Key      string
		ExpireIn time.Duration
		Expected map[string]counter
	}{
		{
			Name: "updates active counter expiry",
			Counters: map[string]counter{
				"key": {count: 5, expireAt: now.Add(time.Hour)},
			},
			Key:      "key",
			ExpireIn: 30 * time.Second,
			Expected: map[string]counter{
				"key": {count: 5, expireAt: now.Add(30 * time.Second)},
			},
		},
		{
			Name: "negative one deletes counter",
			Counters: map[string]counter{
				"key": {count: 5, expireAt: now.Add(time.Hour)},
			},
			Key:      "key",
			ExpireIn: -1,
			Expected: map[string]counter{},
		},
		{
			Name:     "missing counter is ignored",
			Counters: map[string]counter{},
			Key:      "missing",
			ExpireIn: time.Second,
			Expected: map[string]counter{},
		},
		{
			Name: "expired counter is ignored",
			Counters: map[string]counter{
				"key": {count: 5, expireAt: now.Add(-time.Second)},
			},
			Key:      "key",
			ExpireIn: time.Second,
			Expected: map[string]counter{
				"key": {count: 5, expireAt: now.Add(-time.Second)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			mc := testCounter(now, time.Minute, tt.Counters)
			err := mc.Expire(context.Background(), tt.Key, tt.ExpireIn)

			require.NoError(t, err)
			require.Equal(t, tt.Expected, mc.counters)
		})
	}
}

func TestMemoryCounter_DeleteExpiredKeys(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 8, 13, 12, 0, 0, 0, time.UTC)
	mc := testCounter(now, time.Minute, map[string]counter{
		"expired":   {count: 1, expireAt: now.Add(-time.Second)},
		"active":    {count: 2, expireAt: now.Add(time.Second)},
		"unbounded": {count: 3},
	})

	mc.deleteExpiredKeys()

	require.Equal(t, map[string]counter{
		"active":    {count: 2, expireAt: now.Add(time.Second)},
		"unbounded": {count: 3},
	}, mc.counters)
}

func TestMemoryCounter_CountMultipleIncrements(t *testing.T) {
	t.Parallel()

	mc := testCounter(time.Date(2025, 8, 13, 12, 0, 0, 0, time.UTC), time.Minute, map[string]counter{})
	for expected := 1; expected <= 5; expected++ {
		count, ttl, err := mc.Count(context.Background(), "key")
		require.NoError(t, err)
		require.Equal(t, expected, count)
		require.Equal(t, time.Minute, ttl)
	}
}

func TestMemoryCounter_CountConcurrentAccess(t *testing.T) {
	t.Parallel()

	mc := testCounter(time.Date(2025, 8, 13, 12, 0, 0, 0, time.UTC), time.Minute, map[string]counter{})
	const goroutines = 10
	const increments = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range increments {
				_, _, _ = mc.Count(context.Background(), "key")
			}
		}()
	}
	wg.Wait()

	count, ttl, err := mc.Count(context.Background(), "key")
	require.NoError(t, err)
	require.Equal(t, goroutines*increments+1, count)
	require.Equal(t, time.Minute, ttl)
}

func TestNewMemoryCounter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Config   *MemoryCounterConfig
		Interval time.Duration
	}{
		{
			Name:     "with config",
			Config:   &MemoryCounterConfig{Interval: 5 * time.Minute},
			Interval: 5 * time.Minute,
		},
		{Name: "nil config"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			mc := NewMemoryCounter(ctx, tt.Config)

			require.Equal(t, tt.Interval, mc.interval)
			require.NotNil(t, mc.counters)
			require.NotNil(t, mc.timeFunc)
		})
	}
}

func testCounter(now time.Time, interval time.Duration, counters map[string]counter) *MemoryCounter {
	mt := testutil.NewMockTime(now)
	return &MemoryCounter{
		interval: interval,
		counters: counters,
		timeFunc: mt.Now,
	}
}
