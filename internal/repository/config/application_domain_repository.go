package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/config"
)

type gormApplicationDomainRepository struct {
	db *gorm.DB
}

func NewApplicationDomainRepository(db *gorm.DB) *gormApplicationDomainRepository {
	return &gormApplicationDomainRepository{db: db}
}

var _ model.ApplicationDomainRepository = (*gormApplicationDomainRepository)(nil)

func (r *gormApplicationDomainRepository) ListHosts(ctx context.Context, applicationID uuid.UUID) ([]string, error) {
	var hosts []string
	err := r.db.WithContext(ctx).Table("application_domains").
		Where("application_id = ?", applicationID).
		Order("host").
		Pluck("host", &hosts).Error
	if err != nil {
		return nil, err
	}
	return hosts, nil
}

func (r *gormApplicationDomainRepository) SetHosts(ctx context.Context, applicationID uuid.UUID, hosts []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("application_domains").Where("application_id = ?", applicationID).Delete(nil).Error; err != nil {
			return err
		}
		for _, host := range hosts {
			if err := tx.Table("application_domains").Create(map[string]any{
				"application_id": applicationID,
				"host":           host,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *gormApplicationDomainRepository) FindApplicationIDByHost(ctx context.Context, host string) (uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("application_domains").
		Where("host = ?", host).
		Limit(1).
		Pluck("application_id", &ids).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, err
	}
	if len(ids) == 0 {
		return uuid.Nil, nil
	}
	return ids[0], nil
}
