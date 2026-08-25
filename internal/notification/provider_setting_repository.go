package notification

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ProviderSettingRepository is the persistence seam for per-channel provider
// configuration.
type ProviderSettingRepository interface {
	FindByChannel(ctx context.Context, channel string) (*ProviderSetting, error)
	List(ctx context.Context) ([]ProviderSetting, error)
	Create(ctx context.Context, setting *ProviderSetting) error
	Update(ctx context.Context, setting *ProviderSetting) error
}

type gormProviderSettingRepository struct {
	db *gorm.DB
}

func NewProviderSettingRepository(db *gorm.DB) *gormProviderSettingRepository {
	return &gormProviderSettingRepository{db: db}
}

func (r *gormProviderSettingRepository) FindByChannel(ctx context.Context, channel string) (*ProviderSetting, error) {
	var setting ProviderSetting
	err := r.db.WithContext(ctx).Where("channel = ?", channel).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not configured" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func (r *gormProviderSettingRepository) List(ctx context.Context) ([]ProviderSetting, error) {
	var settings []ProviderSetting
	if err := r.db.WithContext(ctx).Order("channel").Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (r *gormProviderSettingRepository) Create(ctx context.Context, setting *ProviderSetting) error {
	return r.db.WithContext(ctx).Create(setting).Error
}

func (r *gormProviderSettingRepository) Update(ctx context.Context, setting *ProviderSetting) error {
	return r.db.WithContext(ctx).Save(setting).Error
}

var _ ProviderSettingRepository = (*gormProviderSettingRepository)(nil)
