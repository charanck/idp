// Package repository is the GORM implementation of model/analytics's
// Repository, spanning the activity and notification tables directly
// (mirroring internal/repository/dashboard's convention of querying other
// packages' tables rather than routing through their repos).
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	activitymodel "controlplane/internal/model/activity"
	model "controlplane/internal/model/analytics"
	notificationmodel "controlplane/internal/model/notification"
)

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *gormRepository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(ctx context.Context, s *model.Snapshot) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *gormRepository) ListSince(ctx context.Context, since time.Time) ([]model.Snapshot, error) {
	var snapshots []model.Snapshot
	err := r.db.WithContext(ctx).Where("captured_at >= ?", since).Order("captured_at ASC").Find(&snapshots).Error
	if err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (r *gormRepository) DeleteOlderThan(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).Where("captured_at < ?", before).Delete(&model.Snapshot{}).Error
}

func (r *gormRepository) CountActivityByTypeSince(ctx context.Context, typ string, since, until time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&activitymodel.Activity{}).
		Where("type = ?", typ).
		Where("timestamp > ?", since).
		Where("timestamp <= ?", until).
		Count(&count).Error
	return count, err
}

func (r *gormRepository) CountNotificationsByStatusSince(ctx context.Context, status string, since, until time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&notificationmodel.Notification{}).
		Where("status = ?", status).
		Where("updated_at > ?", since).
		Where("updated_at <= ?", until).
		Count(&count).Error
	return count, err
}

var _ model.Repository = (*gormRepository)(nil)
