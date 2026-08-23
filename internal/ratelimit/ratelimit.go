// Package ratelimit implements fixed-window rate limiting backed by Redis.
// Every caller uses the same IsRateLimited counter - there is no separate
// failure-only tracking, so a client is throttled purely on request volume
// per window regardless of whether individual attempts succeed or fail.
package ratelimit

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb *redis.Client
}

func NewLimiter(rdb *redis.Client) *Limiter {
	return &Limiter{rdb: rdb}
}

// IsRateLimited allows at most `limit` requests per `window` per (key, clientIP).
func (l *Limiter) IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error) {
	cacheKey := "ratelimit:" + key + ":" + clientIP
	count, err := l.incrWithExpiry(ctx, cacheKey, window)
	if err != nil {
		return false, err
	}
	return count > int64(limit), nil
}

func (l *Limiter) incrWithExpiry(ctx context.Context, key string, window time.Duration) (int64, error) {
	count, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 {
		// Only the first increment in a window sets the expiry, so the
		// window is fixed from the first request, not extended by later ones.
		if err := l.rdb.Expire(ctx, key, window).Err(); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// ClientIP extracts the client IP the same way common/ratelimit.py's
// get_client_ip() does: prefer X-Forwarded-For's first entry, else the
// direct remote address.
func ClientIP(xForwardedFor, remoteAddr string) string {
	if xForwardedFor != "" {
		first, _, _ := strings.Cut(xForwardedFor, ",")
		return strings.TrimSpace(first)
	}
	if remoteAddr == "" {
		return "unknown"
	}
	return remoteAddr
}
