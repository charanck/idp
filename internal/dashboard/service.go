// Package dashboard aggregates counts and recent activity spanning both the
// config and auth packages for the admin dashboard landing page.
package dashboard

import (
	"context"

	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/config"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
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
	db := s.db.WithContext(ctx)
	var c Counts

	if err := db.Model(&config.Application{}).Count(&c.ApplicationCount).Error; err != nil {
		return Counts{}, err
	}
	if err := db.Model(&config.Environment{}).Count(&c.EnvironmentCount).Error; err != nil {
		return Counts{}, err
	}
	if err := db.Model(&config.ConfigEntry{}).Where("is_secret = ?", false).Count(&c.ConfigCount).Error; err != nil {
		return Counts{}, err
	}
	if err := db.Model(&config.ConfigEntry{}).Where("is_secret = ?", true).Count(&c.SecretCount).Error; err != nil {
		return Counts{}, err
	}
	if err := db.Model(&config.FeatureFlag{}).Where("deleted_at IS NULL").Count(&c.FlagCount).Error; err != nil {
		return Counts{}, err
	}
	if err := db.Model(&auth.ServiceClient{}).Count(&c.ClientCount).Error; err != nil {
		return Counts{}, err
	}
	return c, nil
}

// RecentConfigs lists the most recently updated config entries (with
// Application/Environment preloaded), newest first.
func (s *Service) RecentConfigs(ctx context.Context, limit int) ([]config.ConfigEntry, error) {
	var entries []config.ConfigEntry
	err := s.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Order("updated_at DESC").Limit(limit).Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}
