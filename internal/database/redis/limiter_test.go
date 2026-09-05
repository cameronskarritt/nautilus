package redis

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func setupTestRedis(t *testing.T) *Redis {
	t.Helper()
	ctx := context.Background()
	connStr := testutil.RedisConnString(t)
	rdb, err := Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to redis: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func TestLimiter_Count_FillsToCapacity(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)

	capacity := 5
	config := &LimiterConfig{
		Capacity:     capacity,
		Interval:     time.Second,
		IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) { return "test-key", nil },
	}

	limiter := NewLimiter(rdb, config)
	now := time.Unix(0, 0)
	limiter.timeFunc = func() time.Time { return now }

	// Use a unique key for this test
	key := "test-fills-capacity-" + time.Now().Format(time.RFC3339Nano)

	// Fill bucket to capacity
	for i := 1; i <= capacity; i++ {
		level, retry, err := limiter.Count(ctx, key)
		require.NoError(t, err)
		require.Equal(t, i, level)
		require.Equal(t, time.Duration(0), retry)
	}

	// Next request should be rejected
	level, retry, err := limiter.Count(ctx, key)
	require.NoError(t, err)
	require.Equal(t, capacity, level)
	require.Greater(t, retry, time.Duration(0))
}

func TestLimiter_Count_DrainsOverTime(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)

	capacity := 5
	interval := 500 * time.Millisecond // Faster drain for testing
	config := &LimiterConfig{
		Capacity:     capacity,
		Interval:     interval,
		IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) { return "test-key", nil },
	}

	limiter := NewLimiter(rdb, config)
	now := time.Unix(0, 0)
	limiter.timeFunc = func() time.Time { return now }

	// Use a unique key for this test
	key := "test-drains-" + time.Now().Format(time.RFC3339Nano)

	// Fill bucket to capacity
	for i := 1; i <= capacity; i++ {
		_, _, err := limiter.Count(ctx, key)
		require.NoError(t, err)
	}

	// Verify at capacity
	level, retry, err := limiter.Count(ctx, key)
	require.NoError(t, err)
	require.Equal(t, capacity, level)
	require.Greater(t, retry, time.Duration(0))

	// Advance time enough for one more request.
	// With capacity 5 and interval 500ms, leak rate is 10/sec
	// So 1 token drains in 100ms
	now = now.Add(150 * time.Millisecond)

	// Should be able to make another request now
	level, retry, err = limiter.Count(ctx, key)
	require.NoError(t, err)
	require.Equal(t, capacity, level) // Back at capacity after adding 1
	require.Equal(t, time.Duration(0), retry)
}

func TestLimiter_Count_DifferentKeys(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)

	capacity := 3
	config := &LimiterConfig{
		Capacity:     capacity,
		Interval:     time.Second,
		IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) { return "test-key", nil },
	}

	limiter := NewLimiter(rdb, config)
	now := time.Unix(0, 0)
	limiter.timeFunc = func() time.Time { return now }

	// Use unique keys for this test
	key1 := "test-key1-" + time.Now().Format(time.RFC3339Nano)
	key2 := "test-key2-" + time.Now().Format(time.RFC3339Nano)

	// Fill key1 to capacity
	for i := 1; i <= capacity; i++ {
		level, _, err := limiter.Count(ctx, key1)
		require.NoError(t, err)
		require.Equal(t, i, level)
	}

	// key1 should be at capacity
	level, retry, err := limiter.Count(ctx, key1)
	require.NoError(t, err)
	require.Equal(t, capacity, level)
	require.Greater(t, retry, time.Duration(0))

	// key2 should still be empty
	level, retry, err = limiter.Count(ctx, key2)
	require.NoError(t, err)
	require.Equal(t, 1, level)
	require.Equal(t, time.Duration(0), retry)
}

func TestLimiter_Limit(t *testing.T) {
	ctx := context.Background()
	rdb := setupTestRedis(t)

	limiter := NewLimiter(rdb, &LimiterConfig{
		Capacity:     10,
		Interval:     time.Second,
		IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) { return "test-key", nil },
	})

	limit, err := limiter.Limit(ctx, "test-key")
	require.NoError(t, err)
	require.Equal(t, 10, limit)
}

func TestLimiter_Identify(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		Name         string
		IdentityFunc func(ctx context.Context, r *http.Request) (string, error)
		Expected     string
		ExpectError  bool
	}{
		{
			Name: "returns identity from function",
			IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) {
				return "client-123", nil
			},
			Expected:    "client-123",
			ExpectError: false,
		},
		{
			Name: "propagates identity function error",
			IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) {
				return "", http.ErrBodyReadAfterClose
			},
			Expected:    "",
			ExpectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			rdb := setupTestRedis(t)

			config := &LimiterConfig{
				Capacity:     5,
				Interval:     time.Second,
				IdentityFunc: tt.IdentityFunc,
			}

			limiter := NewLimiter(rdb, config)

			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			id, err := limiter.Identify(ctx, req)

			if tt.ExpectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, id)
		})
	}
}

func TestLimiter_Count_ConcurrentCAS(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)

	capacity := 20
	// Use a long interval so the leak rate is negligible over the wall
	// time of the test. With Interval >> test duration, no tokens drain
	// while the goroutines run and we can assert allowed == capacity
	// exactly (no slack needed).
	config := &LimiterConfig{
		Capacity:     capacity,
		Interval:     time.Hour,
		IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) { return "test-key", nil },
	}

	limiter := NewLimiter(rdb, config)

	key := "test-concurrent-cas-" + time.Now().Format(time.RFC3339Nano)

	// Spawn more goroutines than capacity so we force rejections and CAS retries.
	workers := 30
	var (
		wg       sync.WaitGroup
		allowed  atomic.Int32
		rejected atomic.Int32
		failures atomic.Int32
	)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			level, retry, err := limiter.Count(ctx, key)
			if err != nil {
				failures.Add(1)
				return
			}
			if retry == 0 && level <= capacity {
				allowed.Add(1)
			} else {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()

	// No caller should fail; CAS retries with bounded backoff must converge.
	require.Equal(t, int32(0), failures.Load())

	// Every call either allowed or rejected; none lost to CAS storms.
	require.Equal(t, int32(workers), allowed.Load()+rejected.Load())

	// With a one-hour interval, drain during the test window is effectively
	// zero, so exactly capacity tokens are admitted.
	require.Equal(t, int32(capacity), allowed.Load())
}

func TestLimiter_Count_CorruptValue(t *testing.T) {
	ctx := context.Background()

	rdb := setupTestRedis(t)

	config := &LimiterConfig{
		Capacity:     5,
		Interval:     time.Second,
		IdentityFunc: func(_ context.Context, _ *http.Request) (string, error) { return "test-key", nil },
	}

	limiter := NewLimiter(rdb, config)

	key := "test-corrupt-" + time.Now().Format(time.RFC3339Nano)

	tests := []struct {
		Name         string
		Stored       string
		ErrorMessage string
	}{
		{
			Name:         "no separator",
			Stored:       "not-a-valid-bucket",
			ErrorMessage: "malformed bucket",
		},
		{
			Name:         "non-numeric level",
			Stored:       "abc:123",
			ErrorMessage: "parse bucket level",
		},
		{
			Name:         "non-numeric ts",
			Stored:       "1.0:notanumber",
			ErrorMessage: "parse bucket ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			k := key + "-" + tt.Name
			err := rdb.Client().Set(ctx, k, tt.Stored, time.Minute).Err()
			require.NoError(t, err)

			_, _, err = limiter.Count(ctx, k)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.ErrorMessage)
		})
	}
}
