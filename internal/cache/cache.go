// Package cache provides the small key/value + scope-version-counter
// interface config.ConfigService and config.FeatureFlagService cache
// through. The Go server treats Redis as a hard dependency - it's already
// required at startup for sessions and rate limiting, and future work (an
// asynq job queue + asynqmon admin page) will need it too - so RedisCache is
// always used in production. NoopCache remains only as a lightweight
// stand-in for tests that don't care about caching behavior.
package cache

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is deliberately narrow: just what the config-scope caching pattern
// needs (string get/set with TTL, and a "bump this version counter, treating
// absent as 1" operation), not a general-purpose cache API.
type Cache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// GetVersion returns the current value of a scope-version counter,
	// defaulting to 1 if it has never been bumped.
	GetVersion(ctx context.Context, key string) (int64, error)
	// BumpVersion increments a scope-version counter, initializing it to 2
	// (since an absent counter is conceptually version 1) the first time.
	BumpVersion(ctx context.Context, key string) error
}

// RedisCache is the real, CACHE_ENABLED=true backend.
type RedisCache struct {
	rdb    *redis.Client
	prefix string
}

func NewRedisCache(rdb *redis.Client, keyPrefix string) *RedisCache {
	return &RedisCache{rdb: rdb, prefix: keyPrefix}
}

func (c *RedisCache) prefixed(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + ":" + key
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, bool, error) {
	val, err := c.rdb.Get(ctx, c.prefixed(key)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (c *RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.prefixed(key), value, ttl).Err()
}

func (c *RedisCache) GetVersion(ctx context.Context, key string) (int64, error) {
	val, found, err := c.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	if !found {
		return 1, nil
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 1, nil
	}
	return n, nil
}

func (c *RedisCache) BumpVersion(ctx context.Context, key string) error {
	_, found, err := c.Get(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		return c.Set(ctx, key, "2", 0)
	}
	return c.rdb.Incr(ctx, c.prefixed(key)).Err()
}

// NoopCache is a disabled-caching backend: every read misses, every write
// is discarded.
type NoopCache struct{}

func NewNoopCache() *NoopCache { return &NoopCache{} }

func (*NoopCache) Get(context.Context, string) (string, bool, error)        { return "", false, nil }
func (*NoopCache) Set(context.Context, string, string, time.Duration) error { return nil }
func (*NoopCache) GetVersion(context.Context, string) (int64, error)        { return 1, nil }
func (*NoopCache) BumpVersion(context.Context, string) error                { return nil }
