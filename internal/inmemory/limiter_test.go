package inmemory

import (
	"context"
	"net/http"
	"testing"
	"time"

	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestMemoryLimiter_Count(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Capacity int
		Setup    func(context.Context, *MemoryLimiter)
		Level    int
		Retry    time.Duration
	}{
		{Name: "first request", Capacity: 5, Level: 1},
		{
			Name:     "bucket at capacity rejects",
			Capacity: 5,
			Setup: func(ctx context.Context, l *MemoryLimiter) {
				for range 5 {
					_, _, err := l.Count(ctx, "key")
					require.NoError(t, err)
				}
			},
			Level: 5,
			Retry: 200 * time.Millisecond,
		},
		{
			Name:     "partial bucket allows request",
			Capacity: 10,
			Setup: func(ctx context.Context, l *MemoryLimiter) {
				for range 3 {
					_, _, err := l.Count(ctx, "key")
					require.NoError(t, err)
				}
			},
			Level: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			limiter := testLimiter(tt.Capacity, time.Second, time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))
			if tt.Setup != nil {
				tt.Setup(ctx, limiter)
			}

			level, retry, err := limiter.Count(ctx, "key")
			require.NoError(t, err)
			require.Equal(t, tt.Level, level)
			require.Equal(t, tt.Retry, retry)
		})
	}
}

func TestMemoryLimiter_Draining(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mt := testutil.NewMockTime(now)
	limiter := testLimiter(5, time.Second, now)
	limiter.timeFunc = mt.Now

	for range 5 {
		_, _, err := limiter.Count(ctx, "key")
		require.NoError(t, err)
	}

	level, retry, err := limiter.Count(ctx, "key")
	require.NoError(t, err)
	require.Equal(t, 5, level)
	require.Greater(t, retry, time.Duration(0))

	mt.Advance(200 * time.Millisecond)
	level, retry, err = limiter.Count(ctx, "key")
	require.NoError(t, err)
	require.Equal(t, 5, level)
	require.Equal(t, time.Duration(0), retry)

	mt.Advance(time.Second)
	level, retry, err = limiter.Count(ctx, "key")
	require.NoError(t, err)
	require.Equal(t, 1, level)
	require.Equal(t, time.Duration(0), retry)
}

func TestMemoryLimiter_ContinuousDraining(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mt := testutil.NewMockTime(now)
	limiter := testLimiter(10, time.Second, now)
	limiter.timeFunc = mt.Now

	for range 6 {
		_, _, err := limiter.Count(ctx, "key")
		require.NoError(t, err)
	}

	mt.Advance(300 * time.Millisecond)
	level, retry, err := limiter.Count(ctx, "key")
	require.NoError(t, err)
	require.Equal(t, 4, level)
	require.Equal(t, time.Duration(0), retry)
}

func TestMemoryLimiter_DifferentKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limiter := testLimiter(5, time.Second, time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	level, _, err := limiter.Count(ctx, "key1")
	require.NoError(t, err)
	require.Equal(t, 1, level)
	level, _, err = limiter.Count(ctx, "key2")
	require.NoError(t, err)
	require.Equal(t, 1, level)
	level, _, err = limiter.Count(ctx, "key1")
	require.NoError(t, err)
	require.Equal(t, 2, level)
	level, _, err = limiter.Count(ctx, "key2")
	require.NoError(t, err)
	require.Equal(t, 2, level)
}

func TestMemoryLimiter_Limit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limiter := testLimiter(10, time.Minute, time.Now())

	limit, err := limiter.Limit(ctx, "key")

	require.NoError(t, err)
	require.Equal(t, 10, limit)
}

func TestMemoryLimiter_Identify(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expectedErr := http.ErrBodyReadAfterClose
	tests := []struct {
		Name         string
		IdentityFunc IdentityFunc
		Expected     string
		ExpectedErr  error
	}{
		{
			Name: "returns identity",
			IdentityFunc: func(context.Context, *http.Request) (string, error) {
				return "client-123", nil
			},
			Expected: "client-123",
		},
		{
			Name: "returns error",
			IdentityFunc: func(context.Context, *http.Request) (string, error) {
				return "", expectedErr
			},
			ExpectedErr: expectedErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			limiter := testLimiter(5, time.Second, time.Now())
			limiter.identifier = tt.IdentityFunc
			req, err := http.NewRequest(http.MethodGet, "/test", nil)
			require.NoError(t, err)

			id, err := limiter.Identify(ctx, req)
			if tt.ExpectedErr != nil {
				require.ErrorIs(t, err, tt.ExpectedErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, id)
		})
	}
}

func TestNewMemoryLimiter(t *testing.T) {
	t.Parallel()

	t.Run("with config", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		limiter := NewMemoryLimiter(ctx, &MemoryLimiterConfig{
			Capacity:     10,
			Interval:     5 * time.Second,
			IdentityFunc: func(context.Context, *http.Request) (string, error) { return "key", nil },
		})

		require.Equal(t, 10, limiter.capacity)
		require.Equal(t, 5*time.Second, limiter.interval)
		require.NotNil(t, limiter.identifier)
		require.NotNil(t, limiter.buckets)
	})

	t.Run("panics without identity function", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		require.Panics(t, func() {
			NewMemoryLimiter(ctx, &MemoryLimiterConfig{
				Capacity: 10,
				Interval: 5 * time.Second,
			})
		})
	})
}

func TestMemoryLimiter_CleanupOldBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	limiter := testLimiter(5, time.Second, now)
	limiter.buckets = map[string]*bucket{
		"recent": {level: 1, lastTs: now.Add(-500 * time.Millisecond)},
		"old":    {level: 2, lastTs: now.Add(-3 * time.Second)},
	}

	limiter.cleanupOldBuckets()

	require.Contains(t, limiter.buckets, "recent")
	require.NotContains(t, limiter.buckets, "old")
}

func testLimiter(capacity int, interval time.Duration, now time.Time) *MemoryLimiter {
	mt := testutil.NewMockTime(now)
	return &MemoryLimiter{
		capacity:   capacity,
		interval:   interval,
		leakRate:   float64(capacity) / interval.Seconds(),
		identifier: func(context.Context, *http.Request) (string, error) { return "key", nil },
		buckets:    make(map[string]*bucket),
		timeFunc:   mt.Now,
	}
}
