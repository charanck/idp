package repository

import (
	"context"

	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormBrandingRepository struct {
	db *gorm.DB
}

func NewBrandingRepository(db *gorm.DB) *gormBrandingRepository {
	return &gormBrandingRepository{db: db}
}

var _ model.BrandingRepository = (*gormBrandingRepository)(nil)

func (r *gormBrandingRepository) Get(ctx context.Context) (*model.Branding, error) {
	var branding model.Branding
	if err := r.db.WithContext(ctx).First(&branding, "id = ?", 1).Error; err != nil {
		return nil, err
	}
	return &branding, nil
}

func (r *gormBrandingRepository) Update(ctx context.Context, branding *model.Branding) error {
	branding.ID = 1
	return r.db.WithContext(ctx).Save(branding).Error
}
