package analytics

import (
	"context"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// s2sRequestCounterKey is the single Redis key accumulating successfully
// authenticated S2S request volume between snapshot runs.
const s2sRequestCounterKey = "analytics:s2s-request-count"

// Incrementer is the narrow slice of RedisCounter the S2S auth middleware
// needs to bump the request counter.
type Incrementer interface {
	Incr(ctx context.Context) error
}

// CounterReader is the narrow slice of RedisCounter the analytics service
// needs to read and reset the counter when recording a snapshot.
type CounterReader interface {
	GetAndReset(ctx context.Context) (int64, error)
}

// RedisCounter is a plain accumulating counter, separate from cache.Cache
// (whose Get/Set/version-bump interface is deliberately narrower and doesn't
// support INCR or read-and-reset).
type RedisCounter struct {
	rdb *redis.Client
}

func NewRedisCounter(rdb *redis.Client) *RedisCounter {
	return &RedisCounter{rdb: rdb}
}

func (c *RedisCounter) Incr(ctx context.Context) error {
	return c.rdb.Incr(ctx, s2sRequestCounterKey).Err()
}

// GetAndReset atomically reads the counter's current value and resets it to
// zero, so each snapshot only counts requests since the previous one.
func (c *RedisCounter) GetAndReset(ctx context.Context) (int64, error) {
	val, err := c.rdb.SetArgs(ctx, s2sRequestCounterKey, "0", redis.SetArgs{Get: true}).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}
