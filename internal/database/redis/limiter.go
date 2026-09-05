package redis

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"nautilus/internal/errors"
)

// casScript atomically compares the value at KEYS[1] to ARGV[1] and, if equal,
// overwrites it with ARGV[2] and resets the TTL to ARGV[3] seconds. Returns 1
// on swap, 0 on mismatch, and an error sentinel if the key is missing (so the
// caller can fall back to SETNX for first-write).
const casScript = `
local v = redis.call('get', KEYS[1])
if v == false then
  return redis.error_reply("key does not exist")
end
if v ~= ARGV[1] then
  return 0
end
redis.call('setex', KEYS[1], ARGV[3], ARGV[2])
return 1
`

const (
	casMissingKeyErr = "key does not exist"

	// maxCASAttempts bounds the retry count for a single Count() call. Each
	// CAS round produces exactly one winner, so under N-way contention the
	// unlucky tail can lose many rounds before winning. The bound must be
	// generous enough that real-world contention does not surface as a 5xx
	// to callers; the jittered backoff keeps total wall time bounded.
	maxCASAttempts = 30

	// casBackoffBase is the initial sleep between CAS retries. Attempts use
	// randomised exponential backoff to break up thundering-herd collisions
	// when many callers race on the same key.
	casBackoffBase = 200 * time.Microsecond
	casBackoffMax  = 10 * time.Millisecond
)

type LimiterConfig struct {
	Capacity     int           // Max bucket size
	Interval     time.Duration // Time window for full drain
	IdentityFunc func(ctx context.Context, r *http.Request) (string, error)
}

type Limiter struct {
	rdb       *Redis
	casScript *redis.Script
	identity  func(ctx context.Context, r *http.Request) (string, error)
	capacity  int
	interval  time.Duration
	leakRate  float64
	timeFunc  func() time.Time
}

func NewLimiter(rdb *Redis, config *LimiterConfig) *Limiter {
	leakRate := float64(config.Capacity) / config.Interval.Seconds()

	return &Limiter{
		rdb:       rdb,
		casScript: redis.NewScript(casScript),
		identity:  config.IdentityFunc,
		capacity:  config.Capacity,
		interval:  config.Interval,
		leakRate:  leakRate,
		timeFunc:  time.Now,
	}
}

func (rl *Limiter) Count(ctx context.Context, id string) (int, time.Duration, error) {
	ttl := rl.interval
	if ttl < time.Second {
		ttl = time.Second
	}

	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		now := rl.timeFunc()

		raw, found, err := rl.get(ctx, id)
		if err != nil {
			return -1, 0, errors.Wrap(err, "limiter: unable to read bucket")
		}

		var (
			level  float64
			lastTs time.Time
		)
		if found {
			level, lastTs, err = parseBucket(raw)
			if err != nil {
				return -1, 0, err
			}
		} else {
			lastTs = now
		}

		elapsed := math.Max(0, now.Sub(lastTs).Seconds())
		level = math.Max(0, level-elapsed*rl.leakRate)

		allowed := math.Ceil(level) < float64(rl.capacity)
		if allowed {
			level++
		}

		newRaw := formatBucket(level, now)

		var swapped bool
		if !found {
			swapped, err = rl.setNXWithTTL(ctx, id, newRaw, ttl)
		} else {
			swapped, err = rl.compareAndSwap(ctx, id, raw, newRaw, ttl)
		}
		if err != nil {
			return -1, 0, errors.Wrap(err, "limiter: unable to write bucket")
		}
		if !swapped {
			if err := sleepWithBackoff(ctx, attempt); err != nil {
				return -1, 0, err
			}
			continue
		}

		var retryAfter time.Duration
		if !allowed {
			secs := (math.Ceil(level) - float64(rl.capacity) + 1) / rl.leakRate
			retryAfter = time.Duration(secs * float64(time.Second))
		}

		reportedLevel := math.Min(math.Ceil(level), float64(rl.capacity))
		return int(reportedLevel), retryAfter, nil
	}

	return -1, 0, errors.New("limiter: exceeded max CAS attempts")
}

func (rl *Limiter) Limit(_ context.Context, _ string) (int, error) {
	return rl.capacity, nil
}

func (rl *Limiter) Identify(ctx context.Context, r *http.Request) (string, error) {
	return rl.identity(ctx, r)
}

// get returns the raw stored bucket string; found=false signals a missing key
// (i.e. first write) rather than an error.
func (rl *Limiter) get(ctx context.Context, key string) (string, bool, error) {
	v, err := rl.rdb.Client().Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.Wrap(err, "limiter: redis get")
	}
	return v, true, nil
}

// setNXWithTTL does an atomic SET NX with an attached TTL. Returns true if the
// key was created, false if another writer beat us to it.
func (rl *Limiter) setNXWithTTL(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	// SET key value NX EX ttl is atomic and avoids the two-step SETNX+EXPIRE race.
	// go-redis returns redis.Nil when the NX condition is not met.
	_, err := rl.rdb.Client().SetArgs(ctx, key, value, redis.SetArgs{Mode: "NX", TTL: ttl}).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "limiter: redis setnx")
	}
	return true, nil
}

// compareAndSwap atomically swaps value at key from old to new and resets the
// TTL. Returns (false, nil) if the value changed underneath us or the key
// vanished; a genuine Redis error is returned as-is.
func (rl *Limiter) compareAndSwap(ctx context.Context, key, old, new string, ttl time.Duration) (bool, error) {
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	result, err := rl.casScript.Run(ctx, rl.rdb.Client(), []string{key}, old, new, ttlSeconds).Result()
	if err != nil {
		if strings.Contains(err.Error(), casMissingKeyErr) {
			return false, nil
		}
		return false, errors.Wrap(err, "limiter: redis cas script")
	}

	swapped, _ := result.(int64)
	return swapped == 1, nil
}

// formatBucket encodes (level, ts) as "<level>:<unix_nanos>" for CAS-friendly
// storage as a single Redis string.
func formatBucket(level float64, ts time.Time) string {
	return strconv.FormatFloat(level, 'f', 6, 64) + ":" + strconv.FormatInt(ts.UnixNano(), 10)
}

func parseBucket(raw string) (float64, time.Time, error) {
	levelStr, tsStr, ok := strings.Cut(raw, ":")
	if !ok {
		return 0, time.Time{}, errors.Errorf("limiter: malformed bucket %q", raw)
	}

	level, err := strconv.ParseFloat(levelStr, 64)
	if err != nil {
		return 0, time.Time{}, errors.Wrap(err, "limiter: parse bucket level")
	}

	tsNanos, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return 0, time.Time{}, errors.Wrap(err, "limiter: parse bucket ts")
	}

	return level, time.Unix(0, tsNanos), nil
}

// sleepWithBackoff pauses the caller for an exponentially-increasing, jittered
// duration before the next CAS attempt. Returns ctx.Err() if the context is
// cancelled mid-sleep.
func sleepWithBackoff(ctx context.Context, attempt int) error {
	backoff := casBackoffBase << attempt
	if backoff <= 0 || backoff > casBackoffMax {
		backoff = casBackoffMax
	}
	// Full jitter: uniform [0, backoff).
	d := time.Duration(rand.Int64N(int64(backoff)))

	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "limiter: backoff cancelled")
	case <-timer.C:
		return nil
	}
}
