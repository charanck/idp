package dashboard_test

import (
	"context"
	"sort"
	"sync"

	configmodel "controlplane/internal/model/config"
	dashboardmodel "controlplane/internal/model/dashboard"
)

type fakeDashboardRepository struct {
	mu                  sync.Mutex
	applicationCount    int64
	environmentCount    int64
	configCount         int64
	secretCount         int64
	activeFeatureFlags  int64
	serviceClientCount  int64
	recentConfigEntries []configmodel.ConfigEntry
}

func newFakeDashboardRepository() *fakeDashboardRepository {
	return &fakeDashboardRepository{}
}

func (f *fakeDashboardRepository) CountApplications(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applicationCount, nil
}

func (f *fakeDashboardRepository) CountEnvironments(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.environmentCount, nil
}

func (f *fakeDashboardRepository) CountConfigEntries(ctx context.Context, isSecret bool) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if isSecret {
		return f.secretCount, nil
	}
	return f.configCount, nil
}

func (f *fakeDashboardRepository) CountActiveFeatureFlags(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activeFeatureFlags, nil
}

func (f *fakeDashboardRepository) CountServiceClients(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.serviceClientCount, nil
}

func (f *fakeDashboardRepository) RecentConfigEntries(ctx context.Context, limit int) ([]configmodel.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	entries := make([]configmodel.ConfigEntry, len(f.recentConfigEntries))
	copy(entries, f.recentConfigEntries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].UpdatedAt.After(entries[j].UpdatedAt) })
	if limit >= 0 && limit < len(entries) {
		entries = entries[:limit]
	}
	return entries, nil
}

var _ dashboardmodel.Repository = (*fakeDashboardRepository)(nil)
