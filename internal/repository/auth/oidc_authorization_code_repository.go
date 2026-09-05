package repository

import (
	"context"

	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormOIDCAuthorizationCodeRepository struct {
	db *gorm.DB
}

func NewOIDCAuthorizationCodeRepository(db *gorm.DB) *gormOIDCAuthorizationCodeRepository {
	return &gormOIDCAuthorizationCodeRepository{db: db}
}

var _ model.OIDCAuthorizationCodeRepository = (*gormOIDCAuthorizationCodeRepository)(nil)

func (r *gormOIDCAuthorizationCodeRepository) Create(ctx context.Context, code *model.OIDCAuthorizationCode) error {
	return r.db.WithContext(ctx).Create(code).Error
}

// FindAndConsume atomically marks code used in a single conditional UPDATE, so
// a code can never be replayed even under concurrent requests: only the
// caller whose UPDATE actually matched a row (still unused, not expired) gets
// a non-nil result back.
func (r *gormOIDCAuthorizationCodeRepository) FindAndConsume(ctx context.Context, code string) (*model.OIDCAuthorizationCode, error) {
	var authCode model.OIDCAuthorizationCode
	err := r.db.WithContext(ctx).Raw(
		`UPDATE oidc_authorization_codes SET used = true
		 WHERE code = ? AND used = false AND expires_at > now()
		 RETURNING code, service_client_id, user_id, redirect_uri, scope, nonce, used, expires_at, created_at`,
		code,
	).Scan(&authCode).Error
	if err != nil {
		return nil, err
	}
	if authCode.Code == "" {
		return nil, nil
	}
	return &authCode, nil
}
