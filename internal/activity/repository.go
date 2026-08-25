package activity

import (
	"context"

	"gorm.io/gorm"

	"controlplane/internal/config"
)

// Repository is the persistence seam for the append-only activity log.
type Repository interface {
	Create(ctx context.Context, activity *config.Activity) error
	List(ctx context.Context, filter ListFilter) ([]config.Activity, error)
	DistinctResources(ctx context.Context) ([]string, error)
	DistinctTypes(ctx context.Context) ([]string, error)
}

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *gormRepository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(ctx context.Context, a *config.Activity) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *gormRepository) List(ctx context.Context, filter ListFilter) ([]config.Activity, error) {
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
	var activities []config.Activity
	if err := query.Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

func (r *gormRepository) DistinctResources(ctx context.Context) ([]string, error) {
	var resources []string
	err := r.db.WithContext(ctx).Model(&config.Activity{}).Order("resource").Distinct("resource").Pluck("resource", &resources).Error
	if err != nil {
		return nil, err
	}
	return resources, nil
}

func (r *gormRepository) DistinctTypes(ctx context.Context) ([]string, error) {
	var types []string
	err := r.db.WithContext(ctx).Model(&config.Activity{}).Order("type").Distinct("type").Pluck("type", &types).Error
	if err != nil {
		return nil, err
	}
	return types, nil
}

var _ Repository = (*gormRepository)(nil)
