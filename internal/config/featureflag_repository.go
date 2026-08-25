package config

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FeatureFlagRepository is the persistence boundary for FeatureFlag rows.
// Not-found lookups return gorm's raw error (including gorm.ErrRecordNotFound)
// rather than swallowing it - callers decide how to translate that.
//
// FindByScopeAndName and FindActiveByScopeAndName are deliberately separate:
// CreateFlag's undelete-on-recreate behavior needs to see soft-deleted rows
// (no deleted_at filter), while GetFlag/ToggleFlag must never resurrect one.
type FeatureFlagRepository interface {
	FindByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*FeatureFlag, error)
	FindActiveByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*FeatureFlag, error)
	ListActiveByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]FeatureFlag, error)
	Create(ctx context.Context, flag *FeatureFlag) error
	Update(ctx context.Context, flag *FeatureFlag) error
	FindByID(ctx context.Context, id uuid.UUID) (*FeatureFlag, error)
	List(ctx context.Context, filter ListFlagsFilter) ([]FeatureFlag, error)
}

type gormFeatureFlagRepository struct {
	db *gorm.DB
}

func NewFeatureFlagRepository(db *gorm.DB) *gormFeatureFlagRepository {
	return &gormFeatureFlagRepository{db: db}
}

func (r *gormFeatureFlagRepository) FindByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*FeatureFlag, error) {
	var flag FeatureFlag
	err := r.db.WithContext(ctx).
		Where("application_id = ? AND environment_id = ? AND name = ?", applicationID, environmentID, name).
		First(&flag).Error
	if err != nil {
		return nil, err
	}
	return &flag, nil
}

func (r *gormFeatureFlagRepository) FindActiveByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*FeatureFlag, error) {
	var flag FeatureFlag
	err := r.db.WithContext(ctx).
		Where("application_id = ? AND environment_id = ? AND name = ? AND deleted_at IS NULL", applicationID, environmentID, name).
		First(&flag).Error
	if err != nil {
		return nil, err
	}
	return &flag, nil
}

func (r *gormFeatureFlagRepository) ListActiveByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]FeatureFlag, error) {
	var flags []FeatureFlag
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Where("application_id = ? AND environment_id = ? AND deleted_at IS NULL", applicationID, environmentID).
		Find(&flags).Error
	if err != nil {
		return nil, err
	}
	return flags, nil
}

func (r *gormFeatureFlagRepository) Create(ctx context.Context, flag *FeatureFlag) error {
	return r.db.WithContext(ctx).Create(flag).Error
}

func (r *gormFeatureFlagRepository) Update(ctx context.Context, flag *FeatureFlag) error {
	return r.db.WithContext(ctx).Save(flag).Error
}

func (r *gormFeatureFlagRepository) FindByID(ctx context.Context, id uuid.UUID) (*FeatureFlag, error) {
	var flag FeatureFlag
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").First(&flag, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &flag, nil
}

func (r *gormFeatureFlagRepository) List(ctx context.Context, filter ListFlagsFilter) ([]FeatureFlag, error) {
	query := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Joins("JOIN applications ON applications.id = feature_flags.application_id").
		Where("feature_flags.deleted_at IS NULL").
		Order("applications.name, feature_flags.name, feature_flags.description")
	if filter.ApplicationID != nil {
		query = query.Where("feature_flags.application_id = ?", *filter.ApplicationID)
	}
	if filter.EnvironmentID != nil {
		query = query.Where("feature_flags.environment_id = ?", *filter.EnvironmentID)
	}
	if filter.IsEnabled != nil {
		query = query.Where("feature_flags.is_enabled = ?", *filter.IsEnabled)
	}
	var flags []FeatureFlag
	if err := query.Find(&flags).Error; err != nil {
		return nil, err
	}
	return flags, nil
}
