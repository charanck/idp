package model

import "context"

// ProviderSettingRepository is the persistence seam for per-channel provider
// configuration.
type ProviderSettingRepository interface {
	FindByChannel(ctx context.Context, channel string) (*ProviderSetting, error)
	List(ctx context.Context) ([]ProviderSetting, error)
	Create(ctx context.Context, setting *ProviderSetting) error
	Update(ctx context.Context, setting *ProviderSetting) error
}
