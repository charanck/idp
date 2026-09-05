package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormOIDCSigningKeyRepository struct {
	db *gorm.DB
}

func NewOIDCSigningKeyRepository(db *gorm.DB) *gormOIDCSigningKeyRepository {
	return &gormOIDCSigningKeyRepository{db: db}
}

var _ model.OIDCSigningKeyRepository = (*gormOIDCSigningKeyRepository)(nil)

func (r *gormOIDCSigningKeyRepository) Get(ctx context.Context) (*model.OIDCSigningKey, error) {
	var key model.OIDCSigningKey
	if err := r.db.WithContext(ctx).First(&key, "id = ?", 1).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *gormOIDCSigningKeyRepository) GetOrCreate(ctx context.Context, generate func() (*model.OIDCSigningKey, error)) (*model.OIDCSigningKey, error) {
	var key model.OIDCSigningKey
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", "oidc-signing-key").Error; err != nil {
			return err
		}
		if err := tx.First(&key, "id = ?", 1).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			generated, genErr := generate()
			if genErr != nil {
				return genErr
			}
			generated.ID = 1
			key = *generated
			return tx.Create(&key).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &key, nil
}
