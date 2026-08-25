// Package dashboard aggregates counts and recent activity spanning both the
// config and auth packages for the admin dashboard landing page.
package dashboard

import (
	"context"

	"controlplane/internal/config"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Counts bundles the dashboard's summary stat-grid figures.
type Counts struct {
	ApplicationCount int64
	EnvironmentCount int64
	ConfigCount      int64
	SecretCount      int64
	FlagCount        int64
	ClientCount      int64
}

// GetCounts computes the dashboard's summary stat-grid figures.
func (s *Service) GetCounts(ctx context.Context) (Counts, error) {
	var c Counts
	var err error

	if c.ApplicationCount, err = s.repo.CountApplications(ctx); err != nil {
		return Counts{}, err
	}
	if c.EnvironmentCount, err = s.repo.CountEnvironments(ctx); err != nil {
		return Counts{}, err
	}
	if c.ConfigCount, err = s.repo.CountConfigEntries(ctx, false); err != nil {
		return Counts{}, err
	}
	if c.SecretCount, err = s.repo.CountConfigEntries(ctx, true); err != nil {
		return Counts{}, err
	}
	if c.FlagCount, err = s.repo.CountActiveFeatureFlags(ctx); err != nil {
		return Counts{}, err
	}
	if c.ClientCount, err = s.repo.CountServiceClients(ctx); err != nil {
		return Counts{}, err
	}
	return c, nil
}

// RecentConfigs lists the most recently updated config entries (with
// Application/Environment preloaded), newest first.
func (s *Service) RecentConfigs(ctx context.Context, limit int) ([]config.ConfigEntry, error) {
	return s.repo.RecentConfigEntries(ctx, limit)
}
