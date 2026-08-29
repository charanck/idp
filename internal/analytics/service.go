// Package analytics computes and serves the dashboard's trend data: hourly
// gauge/event snapshots written by a DBOS-scheduled workflow (see
// scheduler.go) into a rolling 7-day Postgres window, read through a short
// Redis cache.
package analytics

import (
	"context"
	"encoding/json"
	"time"

	"controlplane/internal/cache"
	"controlplane/internal/dashboard"
	activitymodel "controlplane/internal/model/activity"
	model "controlplane/internal/model/analytics"
	notificationmodel "controlplane/internal/model/notification"
)

// window is how far back RecentSnapshots looks and, correspondingly, how old
// a snapshot can get before DeleteOlderThan prunes it - the rolling window
// the user asked for.
const window = 7 * 24 * time.Hour

// recentCacheTTL is kept well under the hourly snapshot cadence so a cache
// hit never serves data staler than roughly one snapshot cycle.
const recentCacheTTL = 10 * time.Minute

const recentCacheKey = "analytics:recent-snapshots"

// GaugeReader is the narrow slice of *dashboard.Service RecordSnapshot needs
// for the current point-in-time counts.
type GaugeReader interface {
	GetCounts(ctx context.Context) (dashboard.Counts, error)
}

type Service struct {
	repo    model.Repository
	gauges  GaugeReader
	counter CounterReader
	cache   cache.Cache
}

func NewService(repo model.Repository, gauges GaugeReader, counter CounterReader, c cache.Cache) *Service {
	return &Service{repo: repo, gauges: gauges, counter: counter, cache: c}
}

// RecordSnapshot computes and persists one snapshot for the hour ending at
// scheduledTime, then prunes snapshots older than the rolling window.
func (s *Service) RecordSnapshot(ctx context.Context, scheduledTime time.Time) error {
	counts, err := s.gauges.GetCounts(ctx)
	if err != nil {
		return err
	}

	since := scheduledTime.Add(-1 * time.Hour)

	createCount, err := s.repo.CountActivityByTypeSince(ctx, activitymodel.ActivityTypeCreate, since, scheduledTime)
	if err != nil {
		return err
	}
	updateCount, err := s.repo.CountActivityByTypeSince(ctx, activitymodel.ActivityTypeUpdate, since, scheduledTime)
	if err != nil {
		return err
	}
	deleteCount, err := s.repo.CountActivityByTypeSince(ctx, activitymodel.ActivityTypeDelete, since, scheduledTime)
	if err != nil {
		return err
	}
	loginCount, err := s.repo.CountActivityByTypeSince(ctx, activitymodel.ActivityTypeLogin, since, scheduledTime)
	if err != nil {
		return err
	}
	loginFailedCount, err := s.repo.CountActivityByTypeSince(ctx, activitymodel.ActivityTypeLoginFailed, since, scheduledTime)
	if err != nil {
		return err
	}

	sentCount, err := s.repo.CountNotificationsByStatusSince(ctx, notificationmodel.StatusSent, since, scheduledTime)
	if err != nil {
		return err
	}
	failedCount, err := s.repo.CountNotificationsByStatusSince(ctx, notificationmodel.StatusFailed, since, scheduledTime)
	if err != nil {
		return err
	}

	s2sCount, err := s.counter.GetAndReset(ctx)
	if err != nil {
		return err
	}

	snapshot := &model.Snapshot{
		CapturedAt:               scheduledTime,
		ApplicationCount:         counts.ApplicationCount,
		EnvironmentCount:         counts.EnvironmentCount,
		ConfigCount:              counts.ConfigCount,
		SecretCount:              counts.SecretCount,
		FlagCount:                counts.FlagCount,
		ClientCount:              counts.ClientCount,
		ActivityCreateCount:      createCount,
		ActivityUpdateCount:      updateCount,
		ActivityDeleteCount:      deleteCount,
		ActivityLoginCount:       loginCount,
		ActivityLoginFailedCount: loginFailedCount,
		NotificationSentCount:    sentCount,
		NotificationFailedCount:  failedCount,
		S2SRequestCount:          s2sCount,
	}
	if err := s.repo.Create(ctx, snapshot); err != nil {
		return err
	}

	return s.repo.DeleteOlderThan(ctx, scheduledTime.Add(-window))
}

// RecentSnapshots lists snapshots from the last 7 days, oldest first,
// read-through cached in Redis.
func (s *Service) RecentSnapshots(ctx context.Context) ([]model.Snapshot, error) {
	if cached, found, err := s.cache.Get(ctx, recentCacheKey); err == nil && found {
		var snapshots []model.Snapshot
		if err := json.Unmarshal([]byte(cached), &snapshots); err == nil {
			return snapshots, nil
		}
	}

	snapshots, err := s.repo.ListSince(ctx, time.Now().Add(-window))
	if err != nil {
		return nil, err
	}

	if encoded, err := json.Marshal(snapshots); err == nil {
		_ = s.cache.Set(ctx, recentCacheKey, string(encoded), recentCacheTTL)
	}

	return snapshots, nil
}
