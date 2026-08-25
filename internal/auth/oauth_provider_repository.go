package auth

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OAuthProviderRepository is the persistence boundary for OAuthProvider,
// extracted 1:1 from OAuthService's former direct *gorm.DB queries.
type OAuthProviderRepository interface {
	List(ctx context.Context, q string, isActive *bool) ([]OAuthProvider, error)
	FindByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error)
	FindActiveByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error)
	Create(ctx context.Context, p *OAuthProvider) error
	Update(ctx context.Context, p *OAuthProvider) error
	Delete(ctx context.Context, p *OAuthProvider) error
}

type gormOAuthProviderRepository struct {
	db *gorm.DB
}

func NewOAuthProviderRepository(db *gorm.DB) *gormOAuthProviderRepository {
	return &gormOAuthProviderRepository{db: db}
}

func (r *gormOAuthProviderRepository) List(ctx context.Context, q string, isActive *bool) ([]OAuthProvider, error) {
	query := r.db.WithContext(ctx).Order("name")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}
	var providers []OAuthProvider
	if err := query.Find(&providers).Error; err != nil {
		return nil, err
	}
	return providers, nil
}

func (r *gormOAuthProviderRepository) FindByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error) {
	var p OAuthProvider
	if err := r.db.WithContext(ctx).First(&p, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *gormOAuthProviderRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error) {
	var p OAuthProvider
	if err := r.db.WithContext(ctx).First(&p, "id = ? AND is_active = ?", id, true).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *gormOAuthProviderRepository) Create(ctx context.Context, p *OAuthProvider) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *gormOAuthProviderRepository) Update(ctx context.Context, p *OAuthProvider) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *gormOAuthProviderRepository) Delete(ctx context.Context, p *OAuthProvider) error {
	return r.db.WithContext(ctx).Delete(p).Error
}
