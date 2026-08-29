package analytics_test

import (
	"context"
	"testing"
	"time"

	"controlplane/internal/analytics"
	"controlplane/internal/dashboard"
	activitymodel "controlplane/internal/model/activity"
	model "controlplane/internal/model/analytics"
	notificationmodel "controlplane/internal/model/notification"
)

func mkSnapshot(capturedAt time.Time) model.Snapshot {
	return model.Snapshot{CapturedAt: capturedAt}
}

func TestRecordSnapshot_PersistsGaugeAndEventCounts(t *testing.T) {
	repo := newFakeAnalyticsRepository()
	repo.activityCounts[activitymodel.ActivityTypeCreate] = 3
	repo.activityCounts[activitymodel.ActivityTypeUpdate] = 2
	repo.activityCounts[activitymodel.ActivityTypeDelete] = 1
	repo.activityCounts[activitymodel.ActivityTypeLogin] = 5
	repo.activityCounts[activitymodel.ActivityTypeLoginFailed] = 4
	repo.notificationCounts[notificationmodel.StatusSent] = 7
	repo.notificationCounts[notificationmodel.StatusFailed] = 1

	gauges := &fakeGaugeReader{counts: dashboard.Counts{
		ApplicationCount: 1, EnvironmentCount: 2, ConfigCount: 10,
		SecretCount: 3, FlagCount: 4, ClientCount: 5,
	}}
	counter := &fakeCounterReader{value: 42}
	svc := analytics.NewService(repo, gauges, counter, newFakeCache())

	scheduledTime := time.Now().UTC().Truncate(time.Hour)
	if err := svc.RecordSnapshot(context.Background(), scheduledTime); err != nil {
		t.Fatalf("RecordSnapshot: %v", err)
	}

	if len(repo.snapshots) != 1 {
		t.Fatalf("expected 1 snapshot persisted, got %d", len(repo.snapshots))
	}
	snap := repo.snapshots[0]
	if !snap.CapturedAt.Equal(scheduledTime) {
		t.Fatalf("CapturedAt = %v, want %v", snap.CapturedAt, scheduledTime)
	}
	if snap.ApplicationCount != 1 || snap.ClientCount != 5 {
		t.Fatalf("gauge counts not copied: %+v", snap)
	}
	if snap.ActivityCreateCount != 3 || snap.ActivityLoginFailedCount != 4 {
		t.Fatalf("activity counts not copied: %+v", snap)
	}
	if snap.NotificationSentCount != 7 || snap.NotificationFailedCount != 1 {
		t.Fatalf("notification counts not copied: %+v", snap)
	}
	if snap.S2SRequestCount != 42 {
		t.Fatalf("S2SRequestCount = %d, want 42", snap.S2SRequestCount)
	}
}

func TestRecordSnapshot_PrunesSnapshotsOutsideWindow(t *testing.T) {
	repo := newFakeAnalyticsRepository()
	scheduledTime := time.Now().UTC().Truncate(time.Hour)
	repo.snapshots = append(repo.snapshots, mkSnapshot(scheduledTime.Add(-8*24*time.Hour)))

	svc := analytics.NewService(repo, &fakeGaugeReader{}, &fakeCounterReader{}, newFakeCache())
	if err := svc.RecordSnapshot(context.Background(), scheduledTime); err != nil {
		t.Fatalf("RecordSnapshot: %v", err)
	}

	for _, s := range repo.snapshots {
		if s.CapturedAt.Before(scheduledTime.Add(-7 * 24 * time.Hour)) {
			t.Fatalf("expected old snapshot to be pruned, found %+v", s)
		}
	}
}

func TestRecentSnapshots_CachesResult(t *testing.T) {
	repo := newFakeAnalyticsRepository()
	now := time.Now().UTC()
	repo.snapshots = append(repo.snapshots, mkSnapshot(now.Add(-time.Hour)))

	c := newFakeCache()
	svc := analytics.NewService(repo, &fakeGaugeReader{}, &fakeCounterReader{}, c)

	got, err := svc.RecentSnapshots(context.Background())
	if err != nil {
		t.Fatalf("RecentSnapshots: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(got))
	}

	// Mutate the underlying repo directly; a cache hit should still return
	// the previously cached result rather than the new repo state.
	repo.snapshots = append(repo.snapshots, mkSnapshot(now))

	got, err = svc.RecentSnapshots(context.Background())
	if err != nil {
		t.Fatalf("RecentSnapshots (cached): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected cached result with 1 snapshot, got %d", len(got))
	}
}
