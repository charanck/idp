package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/dashboard"
	configmodel "controlplane/internal/model/config"
)

func TestGetCounts_AggregatesAllCounters(t *testing.T) {
	repo := newFakeDashboardRepository()
	repo.applicationCount = 3
	repo.environmentCount = 6
	repo.configCount = 20
	repo.secretCount = 5
	repo.activeFeatureFlags = 4
	repo.serviceClientCount = 2

	svc := dashboard.NewService(repo)

	counts, err := svc.GetCounts(context.Background())
	if err != nil {
		t.Fatalf("GetCounts: %v", err)
	}
	want := dashboard.Counts{
		ApplicationCount: 3,
		EnvironmentCount: 6,
		ConfigCount:      20,
		SecretCount:      5,
		FlagCount:        4,
		ClientCount:      2,
	}
	if counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
}

func TestRecentConfigs_RespectsLimit(t *testing.T) {
	repo := newFakeDashboardRepository()
	now := time.Now()
	for i := 0; i < 5; i++ {
		repo.recentConfigEntries = append(repo.recentConfigEntries, configmodel.ConfigEntry{
			ID:        uuid.New(),
			Key:       "KEY",
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	svc := dashboard.NewService(repo)

	entries, err := svc.RecentConfigs(context.Background(), 2)
	if err != nil {
		t.Fatalf("RecentConfigs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !entries[0].UpdatedAt.After(entries[1].UpdatedAt) {
		t.Fatalf("expected newest-first ordering, got %+v", entries)
	}
}
