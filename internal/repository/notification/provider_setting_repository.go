package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	model "controlplane/internal/model/notification"
)

type gormProviderSettingRepository struct {
	db *gorm.DB
}

func NewProviderSettingRepository(db *gorm.DB) *gormProviderSettingRepository {
	return &gormProviderSettingRepository{db: db}
}

func (r *gormProviderSettingRepository) FindByChannel(ctx context.Context, channel string) (*model.ProviderSetting, error) {
	var setting model.ProviderSetting
	err := r.db.WithContext(ctx).Where("channel = ?", channel).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not configured" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func (r *gormProviderSettingRepository) List(ctx context.Context) ([]model.ProviderSetting, error) {
	var settings []model.ProviderSetting
	if err := r.db.WithContext(ctx).Order("channel").Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (r *gormProviderSettingRepository) Create(ctx context.Context, setting *model.ProviderSetting) error {
	return r.db.WithContext(ctx).Create(setting).Error
}

func (r *gormProviderSettingRepository) Update(ctx context.Context, setting *model.ProviderSetting) error {
	return r.db.WithContext(ctx).Save(setting).Error
}

var _ model.ProviderSettingRepository = (*gormProviderSettingRepository)(nil)
