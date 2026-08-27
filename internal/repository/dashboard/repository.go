package repository

import (
	"context"

	"gorm.io/gorm"

	authmodel "controlplane/internal/model/auth"
	configmodel "controlplane/internal/model/config"
	model "controlplane/internal/model/dashboard"
)

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *gormRepository {
	return &gormRepository{db: db}
}

func (r *gormRepository) CountApplications(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&configmodel.Application{}).Count(&count).Error
	return count, err
}

func (r *gormRepository) CountEnvironments(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&configmodel.Environment{}).Count(&count).Error
	return count, err
}

func (r *gormRepository) CountConfigEntries(ctx context.Context, isSecret bool) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&configmodel.ConfigEntry{}).Where("is_secret = ?", isSecret).Count(&count).Error
	return count, err
}

func (r *gormRepository) CountActiveFeatureFlags(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&configmodel.FeatureFlag{}).Where("deleted_at IS NULL").Count(&count).Error
	return count, err
}

func (r *gormRepository) CountServiceClients(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&authmodel.ServiceClient{}).Count(&count).Error
	return count, err
}

func (r *gormRepository) RecentConfigEntries(ctx context.Context, limit int) ([]configmodel.ConfigEntry, error) {
	var entries []configmodel.ConfigEntry
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Order("updated_at DESC").Limit(limit).Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

var _ model.Repository = (*gormRepository)(nil)
