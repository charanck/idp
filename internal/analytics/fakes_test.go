package analytics_test

import (
	"context"
	"sync"
	"time"

	"controlplane/internal/dashboard"
	model "controlplane/internal/model/analytics"
)

type fakeAnalyticsRepository struct {
	mu        sync.Mutex
	snapshots []model.Snapshot

	activityCounts     map[string]int64
	notificationCounts map[string]int64

	createErr error
}

func newFakeAnalyticsRepository() *fakeAnalyticsRepository {
	return &fakeAnalyticsRepository{
		activityCounts:     map[string]int64{},
		notificationCounts: map[string]int64{},
	}
}

func (f *fakeAnalyticsRepository) Create(ctx context.Context, s *model.Snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.snapshots = append(f.snapshots, *s)
	return nil
}

func (f *fakeAnalyticsRepository) ListSince(ctx context.Context, since time.Time) ([]model.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Snapshot
	for _, s := range f.snapshots {
		if !s.CapturedAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeAnalyticsRepository) DeleteOlderThan(ctx context.Context, before time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []model.Snapshot
	for _, s := range f.snapshots {
		if !s.CapturedAt.Before(before) {
			kept = append(kept, s)
		}
	}
	f.snapshots = kept
	return nil
}

func (f *fakeAnalyticsRepository) CountActivityByTypeSince(ctx context.Context, typ string, since, until time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activityCounts[typ], nil
}

func (f *fakeAnalyticsRepository) CountNotificationsByStatusSince(ctx context.Context, status string, since, until time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.notificationCounts[status], nil
}

var _ model.Repository = (*fakeAnalyticsRepository)(nil)

type fakeGaugeReader struct {
	counts dashboard.Counts
}

func (f *fakeGaugeReader) GetCounts(ctx context.Context) (dashboard.Counts, error) {
	return f.counts, nil
}

type fakeCounterReader struct {
	mu    sync.Mutex
	value int64
	err   error
}

func (f *fakeCounterReader) GetAndReset(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	v := f.value
	f.value = 0
	return v, nil
}

type fakeCache struct {
	mu    sync.Mutex
	store map[string]string
}

func newFakeCache() *fakeCache {
	return &fakeCache{store: map[string]string{}}
}

func (c *fakeCache) Get(ctx context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.store[key]
	return v, ok, nil
}

func (c *fakeCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
	return nil
}

func (c *fakeCache) GetVersion(ctx context.Context, key string) (int64, error) { return 1, nil }
func (c *fakeCache) BumpVersion(ctx context.Context, key string) error         { return nil }
