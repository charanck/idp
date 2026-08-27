package repository

import (
	"context"

	"gorm.io/gorm"

	model "controlplane/internal/model/activity"
)

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *gormRepository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(ctx context.Context, a *model.Activity) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *gormRepository) List(ctx context.Context, filter model.ListFilter) ([]model.Activity, error) {
	query := r.db.WithContext(ctx).Order("timestamp DESC")
	if filter.Resource != "" {
		query = query.Where("resource = ?", filter.Resource)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.UserLike != "" {
		query = query.Where("user_email ILIKE ?", "%"+filter.UserLike+"%")
	}
	var activities []model.Activity
	if err := query.Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

func (r *gormRepository) DistinctResources(ctx context.Context) ([]string, error) {
	var resources []string
	err := r.db.WithContext(ctx).Model(&model.Activity{}).Order("resource").Distinct("resource").Pluck("resource", &resources).Error
	if err != nil {
		return nil, err
	}
	return resources, nil
}

func (r *gormRepository) DistinctTypes(ctx context.Context) ([]string, error) {
	var types []string
	err := r.db.WithContext(ctx).Model(&model.Activity{}).Order("type").Distinct("type").Pluck("type", &types).Error
	if err != nil {
		return nil, err
	}
	return types, nil
}

var _ model.Repository = (*gormRepository)(nil)
