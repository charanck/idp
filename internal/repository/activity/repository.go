package repository

import (
	"context"
	"time"

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
	// details is a large free-text column no list view renders; skip it.
	query := r.db.WithContext(ctx).Omit("details").Order("timestamp DESC")
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
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

func (r *gormRepository) DeleteOlderThan(ctx context.Context, before time.Time) error {
	// Deleted in bounded batches: one giant DELETE holds a single long
	// transaction, bursts WAL, and bloats every index on the table at once.
	for {
		res := r.db.WithContext(ctx).Exec(
			`DELETE FROM activities WHERE ctid IN (SELECT ctid FROM activities WHERE "timestamp" < ? LIMIT ?)`,
			before, deleteBatchSize)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected < deleteBatchSize {
			return nil
		}
	}
}

// deleteBatchSize bounds how many rows a single retention DELETE removes.
const deleteBatchSize = 5000

var _ model.Repository = (*gormRepository)(nil)
