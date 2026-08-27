package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormOAuthUserTokenRepository struct {
	db *gorm.DB
}

func NewOAuthUserTokenRepository(db *gorm.DB) *gormOAuthUserTokenRepository {
	return &gormOAuthUserTokenRepository{db: db}
}

var _ model.OAuthUserTokenRepository = (*gormOAuthUserTokenRepository)(nil)

func (r *gormOAuthUserTokenRepository) FindByProviderAndProviderUserID(ctx context.Context, providerID uuid.UUID, providerUserID string) (*model.OAuthUserToken, error) {
	var t model.OAuthUserToken
	err := r.db.WithContext(ctx).
		Where("provider_id = ? AND provider_user_id = ?", providerID, providerUserID).
		First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *gormOAuthUserTokenRepository) Create(ctx context.Context, t *model.OAuthUserToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *gormOAuthUserTokenRepository) Update(ctx context.Context, t *model.OAuthUserToken) error {
	return r.db.WithContext(ctx).Save(t).Error
}
