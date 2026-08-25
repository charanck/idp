package dashboard

import (
	"context"

	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/config"
)

// Repository is the persistence seam for the dashboard's aggregate counts and
// recent-activity queries, spanning both the config and auth models.
type Repository interface {
	CountApplications(ctx context.Context) (int64, error)
	CountEnvironments(ctx context.Context) (int64, error)
	CountConfigEntries(ctx context.Context, isSecret bool) (int64, error)
	CountActiveFeatureFlags(ctx context.Context) (int64, error)
	CountServiceClients(ctx context.Context) (int64, error)
	RecentConfigEntries(ctx context.Context, limit int) ([]config.ConfigEntry, error)
}

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *gormRepository {
	return &gormRepository{db: db}
}

func (r *gormRepository) CountApplications(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&config.Application{}).Count(&count).Error
	return count, err
}

func (r *gormRepository) CountEnvironments(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&config.Environment{}).Count(&count).Error
	return count, err
}

func (r *gormRepository) CountConfigEntries(ctx context.Context, isSecret bool) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&config.ConfigEntry{}).Where("is_secret = ?", isSecret).Count(&count).Error
	return count, err
}

func (r *gormRepository) CountActiveFeatureFlags(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&config.FeatureFlag{}).Where("deleted_at IS NULL").Count(&count).Error
	return count, err
}

func (r *gormRepository) CountServiceClients(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&auth.ServiceClient{}).Count(&count).Error
	return count, err
}

func (r *gormRepository) RecentConfigEntries(ctx context.Context, limit int) ([]config.ConfigEntry, error) {
	var entries []config.ConfigEntry
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Order("updated_at DESC").Limit(limit).Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

var _ Repository = (*gormRepository)(nil)
