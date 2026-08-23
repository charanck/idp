package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestLimiter(t *testing.T) *Limiter {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return NewLimiter(rdb)
}

func TestIsRateLimited_AllowsUpToLimit(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		limited, err := l.IsRateLimited(ctx, "bucket", "1.2.3.4", 3, time.Minute)
		if err != nil {
			t.Fatalf("IsRateLimited: %v", err)
		}
		if limited {
			t.Fatalf("request %d should not be limited yet", i+1)
		}
	}

	limited, err := l.IsRateLimited(ctx, "bucket", "1.2.3.4", 3, time.Minute)
	if err != nil {
		t.Fatalf("IsRateLimited: %v", err)
	}
	if !limited {
		t.Fatal("4th request should be limited")
	}
}

func TestIsRateLimited_SeparateKeysAndIPsIndependent(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()

	l.IsRateLimited(ctx, "bucket-a", "1.1.1.1", 1, time.Minute)
	limited, _ := l.IsRateLimited(ctx, "bucket-b", "1.1.1.1", 1, time.Minute)
	if limited {
		t.Fatal("different bucket key should have its own counter")
	}

	limited, _ = l.IsRateLimited(ctx, "bucket-a", "2.2.2.2", 1, time.Minute)
	if limited {
		t.Fatal("different client IP should have its own counter")
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		xff, remote, want string
	}{
		{"", "10.0.0.1", "10.0.0.1"},
		{"", "", "unknown"},
		{"203.0.113.5", "10.0.0.1", "203.0.113.5"},
		{"203.0.113.5, 10.0.0.1", "10.0.0.1", "203.0.113.5"},
		{" 203.0.113.5 , 10.0.0.1", "10.0.0.1", "203.0.113.5"},
	}
	for _, c := range cases {
		got := ClientIP(c.xff, c.remote)
		if got != c.want {
			t.Errorf("ClientIP(%q, %q) = %q, want %q", c.xff, c.remote, got, c.want)
		}
	}
}
