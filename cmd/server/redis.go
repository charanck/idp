package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"controlplane/internal/appconfig"
	"controlplane/internal/observability"
)

// newRedisClient connects to Redis, installs OTEL instrumentation when
// enabled, and pings to fail fast on a bad REDIS_URL.
func newRedisClient(cfg *appconfig.Config) (*redis.Client, error) {
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(redisOpts)

	if observability.Enabled() {
		if err := redisotel.InstrumentTracing(rdb); err != nil {
			return nil, fmt.Errorf("install redis otel tracing: %w", err)
		}
		if err := redisotel.InstrumentMetrics(rdb); err != nil {
			return nil, fmt.Errorf("install redis otel metrics: %w", err)
		}
	}

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis: %w", err)
	}

	return rdb, nil
}
