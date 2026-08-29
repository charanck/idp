package analytics

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestCounter(t *testing.T) *RedisCounter {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return NewRedisCounter(rdb)
}

func TestRedisCounter_GetAndResetWithNoIncrementsReturnsZero(t *testing.T) {
	c := newTestCounter(t)
	ctx := context.Background()

	got, err := c.GetAndReset(ctx)
	if err != nil {
		t.Fatalf("GetAndReset: %v", err)
	}
	if got != 0 {
		t.Fatalf("got = %d, want 0", got)
	}
}

func TestRedisCounter_IncrAccumulatesThenGetAndResetZeroesIt(t *testing.T) {
	c := newTestCounter(t)
	ctx := context.Background()

	for range 5 {
		if err := c.Incr(ctx); err != nil {
			t.Fatalf("Incr: %v", err)
		}
	}

	got, err := c.GetAndReset(ctx)
	if err != nil {
		t.Fatalf("GetAndReset: %v", err)
	}
	if got != 5 {
		t.Fatalf("got = %d, want 5", got)
	}

	got, err = c.GetAndReset(ctx)
	if err != nil {
		t.Fatalf("GetAndReset (second call): %v", err)
	}
	if got != 0 {
		t.Fatalf("got = %d, want 0 after reset", got)
	}
}
