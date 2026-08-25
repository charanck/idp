package auth

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OAuthUserTokenRepository is the persistence boundary for OAuthUserToken,
// extracted 1:1 from OAuthService.AuthenticateOrCreateUser's former direct
// *gorm.DB queries.
type OAuthUserTokenRepository interface {
	FindByProviderAndProviderUserID(ctx context.Context, providerID uuid.UUID, providerUserID string) (*OAuthUserToken, error)
	Create(ctx context.Context, t *OAuthUserToken) error
	Update(ctx context.Context, t *OAuthUserToken) error
}

type gormOAuthUserTokenRepository struct {
	db *gorm.DB
}

func NewOAuthUserTokenRepository(db *gorm.DB) *gormOAuthUserTokenRepository {
	return &gormOAuthUserTokenRepository{db: db}
}

func (r *gormOAuthUserTokenRepository) FindByProviderAndProviderUserID(ctx context.Context, providerID uuid.UUID, providerUserID string) (*OAuthUserToken, error) {
	var t OAuthUserToken
	err := r.db.WithContext(ctx).
		Where("provider_id = ? AND provider_user_id = ?", providerID, providerUserID).
		First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *gormOAuthUserTokenRepository) Create(ctx context.Context, t *OAuthUserToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *gormOAuthUserTokenRepository) Update(ctx context.Context, t *OAuthUserToken) error {
	return r.db.WithContext(ctx).Save(t).Error
}
