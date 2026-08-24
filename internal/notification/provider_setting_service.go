package notification

import (
	"context"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"controlplane/internal/crypto"
)

// ProviderSettingService manages per-channel provider configuration.
// DecryptCredentials is only ever called from the worker's send path, never
// from a handler - the admin UI only shows a masked "configured" indicator,
// mirroring ConfigService's "***ENCRYPTED***" convention.
type ProviderSettingService struct {
	db         *gorm.DB
	encryption *crypto.EncryptionService
}

func NewProviderSettingService(db *gorm.DB, encryption *crypto.EncryptionService) *ProviderSettingService {
	return &ProviderSettingService{db: db, encryption: encryption}
}

// Get returns the provider setting for a channel, or nil if never configured.
func (s *ProviderSettingService) Get(ctx context.Context, channel string) (*ProviderSetting, error) {
	var setting ProviderSetting
	err := s.db.WithContext(ctx).Where("channel = ?", channel).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not configured" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

// List returns provider settings for all channels that have been configured.
func (s *ProviderSettingService) List(ctx context.Context) ([]ProviderSetting, error) {
	var settings []ProviderSetting
	if err := s.db.WithContext(ctx).Order("channel").Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

// UpsertInput bundles Upsert's parameters. A blank Credentials means "leave
// unchanged" when updating an existing setting, matching
// oauthProviderFormFromRequest's blank-client-secret convention.
type UpsertInput struct {
	Channel     string
	Config      datatypes.JSON
	Credentials string
	IsActive    bool
}

// Upsert creates or updates a channel's provider setting, encrypting
// Credentials with the master key before storage.
func (s *ProviderSettingService) Upsert(ctx context.Context, in UpsertInput) (*ProviderSetting, error) {
	existing, err := s.Get(ctx, in.Channel)
	if err != nil {
		return nil, err
	}

	encryptedCredentials := ""
	if existing != nil {
		encryptedCredentials = existing.Credentials
	}
	if in.Credentials != "" {
		encryptedCredentials, err = s.encryption.EncryptForStorage(in.Credentials)
		if err != nil {
			return nil, err
		}
	}

	if existing == nil {
		setting := &ProviderSetting{
			Channel:     in.Channel,
			Config:      in.Config,
			Credentials: encryptedCredentials,
			IsActive:    in.IsActive,
		}
		if err := s.db.WithContext(ctx).Create(setting).Error; err != nil {
			return nil, err
		}
		return setting, nil
	}

	existing.Config = in.Config
	existing.Credentials = encryptedCredentials
	existing.IsActive = in.IsActive
	if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
		return nil, err
	}
	return existing, nil
}

// DecryptCredentials decrypts a provider setting's credentials for use by
// the worker's send path only.
func (s *ProviderSettingService) DecryptCredentials(setting *ProviderSetting) (string, error) {
	if setting.Credentials == "" {
		return "", nil
	}
	return s.encryption.DecryptFromStorage(setting.Credentials)
}
