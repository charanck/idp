package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormOAuthProviderRepository struct {
	db *gorm.DB
}

func NewOAuthProviderRepository(db *gorm.DB) *gormOAuthProviderRepository {
	return &gormOAuthProviderRepository{db: db}
}

var _ model.OAuthProviderRepository = (*gormOAuthProviderRepository)(nil)

func (r *gormOAuthProviderRepository) List(ctx context.Context, q string, isActive *bool) ([]model.OAuthProvider, error) {
	query := r.db.WithContext(ctx).Order("name")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}
	var providers []model.OAuthProvider
	if err := query.Find(&providers).Error; err != nil {
		return nil, err
	}
	return providers, nil
}

func (r *gormOAuthProviderRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.OAuthProvider, error) {
	var p model.OAuthProvider
	if err := r.db.WithContext(ctx).First(&p, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *gormOAuthProviderRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*model.OAuthProvider, error) {
	var p model.OAuthProvider
	if err := r.db.WithContext(ctx).First(&p, "id = ? AND is_active = ?", id, true).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *gormOAuthProviderRepository) Create(ctx context.Context, p *model.OAuthProvider) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *gormOAuthProviderRepository) Update(ctx context.Context, p *model.OAuthProvider) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *gormOAuthProviderRepository) Delete(ctx context.Context, p *model.OAuthProvider) error {
	return r.db.WithContext(ctx).Delete(p).Error
}
