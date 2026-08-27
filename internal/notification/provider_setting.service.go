package notification

import (
	"context"

	"gorm.io/datatypes"

	"controlplane/internal/crypto"
	model "controlplane/internal/model/notification"
)

// ProviderSettingService manages per-channel provider configuration.
// DecryptCredentials is only ever called from the worker's send path, never
// from a handler - the admin UI only shows a masked "configured" indicator,
// mirroring ConfigService's "***ENCRYPTED***" convention.
type ProviderSettingService struct {
	repo       model.ProviderSettingRepository
	encryption *crypto.EncryptionService
}

func NewProviderSettingService(repo model.ProviderSettingRepository, encryption *crypto.EncryptionService) *ProviderSettingService {
	return &ProviderSettingService{repo: repo, encryption: encryption}
}

// Get returns the provider setting for a channel, or nil if never configured.
func (s *ProviderSettingService) Get(ctx context.Context, channel string) (*model.ProviderSetting, error) {
	return s.repo.FindByChannel(ctx, channel)
}

// List returns provider settings for all channels that have been configured.
func (s *ProviderSettingService) List(ctx context.Context) ([]model.ProviderSetting, error) {
	return s.repo.List(ctx)
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
func (s *ProviderSettingService) Upsert(ctx context.Context, in UpsertInput) (*model.ProviderSetting, error) {
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
		setting := &model.ProviderSetting{
			Channel:     in.Channel,
			Config:      in.Config,
			Credentials: encryptedCredentials,
			IsActive:    in.IsActive,
		}
		if err := s.repo.Create(ctx, setting); err != nil {
			return nil, err
		}
		return setting, nil
	}

	existing.Config = in.Config
	existing.Credentials = encryptedCredentials
	existing.IsActive = in.IsActive
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DecryptCredentials decrypts a provider setting's credentials for use by
// the worker's send path only.
func (s *ProviderSettingService) DecryptCredentials(setting *model.ProviderSetting) (string, error) {
	if setting.Credentials == "" {
		return "", nil
	}
	return s.encryption.DecryptFromStorage(setting.Credentials)
}
