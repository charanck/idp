package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/cache"
	"controlplane/internal/crypto"
	model "controlplane/internal/model/config"
)

// ConfigService mirrors config_management/services.py's ConfigService.
type ConfigService struct {
	configs      model.ConfigRepository
	apps         model.ApplicationRepository
	envs         model.EnvironmentRepository
	encryption   *crypto.EncryptionService
	cache        cache.Cache
	cacheTimeout time.Duration
}

func NewConfigService(configs model.ConfigRepository, apps model.ApplicationRepository, envs model.EnvironmentRepository, encryption *crypto.EncryptionService, c cache.Cache, cacheTimeout time.Duration) *ConfigService {
	return &ConfigService{configs: configs, apps: apps, envs: envs, encryption: encryption, cache: c, cacheTimeout: cacheTimeout}
}

// getScope looks up an existing Application/Environment without creating
// them; either return value is nil if not found.
func (s *ConfigService) getScope(ctx context.Context, service, environment string) (*model.Application, *model.Environment, error) {
	app, err := s.apps.FindByName(ctx, service)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	env, err := s.envs.FindByApplicationAndName(ctx, app.ID, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return app, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return app, env, nil
}

func scopeVersionKey(service, environment string) string {
	return fmt.Sprintf("config:scope-version:%s:%s", service, environment)
}

func (s *ConfigService) getScopeVersion(ctx context.Context, service, environment string) (int64, error) {
	return s.cache.GetVersion(ctx, scopeVersionKey(service, environment))
}

// InvalidateScopeCache bumps the scope-version counter for a service/environment,
// invalidating any cached config-list payloads for it.
func (s *ConfigService) InvalidateScopeCache(ctx context.Context, service, environment string) error {
	return s.cache.BumpVersion(ctx, scopeVersionKey(service, environment))
}

// RecordConfigVersion snapshots a ConfigEntry's current (encrypted) value
// into history. Not called on delete - history cascades away with the entry.
func (s *ConfigService) RecordConfigVersion(ctx context.Context, config *model.ConfigEntry, action string, changedBy string) (*model.ConfigEntryVersion, error) {
	return s.configs.RecordVersion(ctx, config, action, changedBy)
}

// UpsertOptions bundles upsert_config's optional parameters.
type UpsertOptions struct {
	IsSecret      bool
	ConfigType    string // defaults to TypeString if empty
	ChangedBy     string
	HistoryAction string // defaults to "create"/"update" based on whether the entry existed
}

// UpsertConfig creates or updates a configuration/secret entry, mirroring
// ConfigService.upsert_config.
func (s *ConfigService) UpsertConfig(ctx context.Context, service, environment, key, value string, opts UpsertOptions) (*model.ConfigEntry, error) {
	encryptedValue, err := s.encryption.EncryptForStorage(value)
	if err != nil {
		return nil, err
	}

	configType := opts.ConfigType
	if configType == "" {
		configType = model.TypeString
	}

	entry, historyAction, err := s.configs.UpsertEntryAndRecordVersion(ctx, model.UpsertEntryParams{
		Service:        service,
		Environment:    environment,
		Key:            key,
		EncryptedValue: encryptedValue,
		ConfigType:     configType,
		IsSecret:       opts.IsSecret,
		HistoryAction:  opts.HistoryAction,
		ChangedBy:      opts.ChangedBy,
	})
	if err != nil {
		return nil, err
	}

	if err := s.InvalidateScopeCache(ctx, service, environment); err != nil {
		return nil, err
	}

	slog.Info("upserted config", "action", historyAction, "service", service, "environment", environment, "key", key, "secret", opts.IsSecret)
	return entry, nil
}

// GetConfig returns a specific configuration or secret, or nil if not found.
func (s *ConfigService) GetConfig(ctx context.Context, service, environment, key string) (*model.ConfigEntry, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return nil, nil
	}

	entry, err := s.configs.FindByScopeAndKey(ctx, app.ID, env.ID, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// GetConfigWithScope returns a specific config entry with its Application/Environment preloaded.
func (s *ConfigService) GetConfigWithScope(ctx context.Context, service, environment, key string) (*model.ConfigEntry, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return nil, nil
	}

	entry, err := s.configs.FindByScopeAndKeyWithScope(ctx, app.ID, env.ID, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// ListConfigs lists all configurations for a service/environment.
func (s *ConfigService) ListConfigs(ctx context.Context, service, environment string) ([]model.ConfigEntry, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return []model.ConfigEntry{}, nil
	}

	return s.configs.ListByScope(ctx, app.ID, env.ID)
}

// DecryptConfigValue decrypts a config value for internal use.
func (s *ConfigService) DecryptConfigValue(entry *model.ConfigEntry) (string, error) {
	return s.encryption.DecryptFromStorage(entry.Value)
}

// DecryptConfigValueOrOriginal decrypts a value when possible, falling back
// to the stored (legacy plaintext) value on an invalid token.
func (s *ConfigService) DecryptConfigValueOrOriginal(entry *model.ConfigEntry) string {
	value, err := s.DecryptConfigValue(entry)
	if errors.Is(err, crypto.ErrInvalidToken) {
		return entry.Value
	}
	if err != nil {
		return entry.Value
	}
	return value
}

// DeleteConfig deletes a configuration entry (and its version history, via
// cascading FK). Returns false if the ID was malformed or not found.
func (s *ConfigService) DeleteConfig(ctx context.Context, configID string) (bool, error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		slog.Warn("config delete failed: malformed id", "id", configID)
		return false, nil
	}

	entry, err := s.configs.FindByIDWithScope(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("config delete failed: not found", "id", configID)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := s.configs.Delete(ctx, entry); err != nil {
		return false, err
	}
	if err := s.InvalidateScopeCache(ctx, entry.Application.Name, entry.Environment.Name); err != nil {
		return false, err
	}
	slog.Info("deleted config", "service", entry.Application.Name, "environment", entry.Environment.Name, "key", entry.Key)
	return true, nil
}

// GetConfigHistory lists version history for a config entry, newest first.
func (s *ConfigService) GetConfigHistory(ctx context.Context, configID string) ([]model.ConfigEntryVersion, error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return []model.ConfigEntryVersion{}, nil
	}

	if _, err := s.configs.FindByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []model.ConfigEntryVersion{}, nil
		}
		return nil, err
	}

	return s.configs.ListVersions(ctx, id)
}

// DecryptVersionValue decrypts a historical version's value for internal use.
func (s *ConfigService) DecryptVersionValue(version *model.ConfigEntryVersion) (string, error) {
	return s.encryption.DecryptFromStorage(version.Value)
}

// RollbackConfig restores a prior version by writing it again via
// UpsertConfig, so the rollback itself becomes a new, auditable version.
func (s *ConfigService) RollbackConfig(ctx context.Context, configID string, version int, changedBy string) (*model.ConfigEntry, error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return nil, nil //nolint:nilnil // malformed/not-found id is a valid "not found" outcome, matching the Python service.
	}

	entry, err := s.configs.FindByIDWithScope(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	target, err := s.configs.FindVersion(ctx, entry.ID, version)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	decryptedValue, err := s.encryption.DecryptFromStorage(target.Value)
	if err != nil {
		return nil, err
	}

	updated, err := s.UpsertConfig(ctx, entry.Application.Name, entry.Environment.Name, entry.Key, decryptedValue, UpsertOptions{
		IsSecret:      target.IsSecret,
		ConfigType:    target.Type,
		ChangedBy:     changedBy,
		HistoryAction: model.ActionRollback,
	})
	if err != nil || updated == nil {
		return updated, err
	}
	updated.Application = entry.Application
	updated.Environment = entry.Environment
	return updated, nil
}

// ClientConfig is a config entry decrypted then re-encrypted for a specific
// service client, ready to hand back over the S2S API.
type ClientConfig struct {
	ID          string `json:"id"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Type        string `json:"type"`
	IsSecret    bool   `json:"is_secret"`
}

// GetConfigForClient returns a single config encrypted for a specific client.
func (s *ConfigService) GetConfigForClient(ctx context.Context, service, environment, key, clientEncryptionKey string) (*ClientConfig, error) {
	entry, err := s.GetConfigWithScope(ctx, service, environment, key)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	decryptedValue, err := s.encryption.DecryptFromStorage(entry.Value)
	if err != nil {
		slog.Error("failed to decrypt config: value is not valid under MASTER_ENCRYPTION_KEY", "service", service, "environment", environment, "key", key)
		return nil, fmt.Errorf("config %q could not be decrypted", key)
	}

	clientEncryptedValue, err := s.encryption.ReEncryptForClient(decryptedValue, clientEncryptionKey)
	if err != nil {
		return nil, err
	}

	return &ClientConfig{
		ID:          entry.ID.String(),
		Service:     entry.Application.Name,
		Environment: entry.Environment.Name,
		Key:         entry.Key,
		Value:       clientEncryptedValue,
		Type:        entry.Type,
		IsSecret:    entry.IsSecret,
	}, nil
}

// ListConfigsForClient lists all configs for a service/environment, each
// encrypted for a specific client, using the scope-version cache.
func (s *ConfigService) ListConfigsForClient(ctx context.Context, service, environment, clientEncryptionKey string) ([]ClientConfig, error) {
	sum := sha256.Sum256([]byte(clientEncryptionKey))
	clientKeyHash := hex.EncodeToString(sum[:])[:16]

	scopeVersion, err := s.getScopeVersion(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("config:list:%s:%s:%s:v%d", service, environment, clientKeyHash, scopeVersion)

	if cached, found, err := s.cache.Get(ctx, cacheKey); err != nil {
		return nil, err
	} else if found {
		var result []ClientConfig
		if err := json.Unmarshal([]byte(cached), &result); err == nil {
			return result, nil
		}
	}

	entries, err := s.ListConfigs(ctx, service, environment)
	if err != nil {
		return nil, err
	}

	result := make([]ClientConfig, 0, len(entries))
	for _, entry := range entries {
		decryptedValue, err := s.encryption.DecryptFromStorage(entry.Value)
		if err != nil {
			// One corrupted/undecryptable row shouldn't take down the whole
			// list for every client polling this scope - skip and flag it.
			slog.Error("skipping config in client list: value is not valid under MASTER_ENCRYPTION_KEY", "service", service, "environment", environment, "key", entry.Key)
			continue
		}

		clientEncryptedValue, err := s.encryption.ReEncryptForClient(decryptedValue, clientEncryptionKey)
		if err != nil {
			return nil, err
		}

		result = append(result, ClientConfig{
			ID:          entry.ID.String(),
			Service:     entry.Application.Name,
			Environment: entry.Environment.Name,
			Key:         entry.Key,
			Value:       clientEncryptedValue,
			Type:        entry.Type,
			IsSecret:    entry.IsSecret,
		})
	}

	if serialized, err := json.Marshal(result); err == nil {
		if err := s.cache.Set(ctx, cacheKey, string(serialized), s.cacheTimeout); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// GetConfigByID returns a config entry by ID with its Application/Environment
// preloaded, or nil if not found.
func (s *ConfigService) GetConfigByID(ctx context.Context, id uuid.UUID) (*model.ConfigEntry, error) {
	entry, err := s.configs.FindByIDWithScope(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// ListAllConfigEntries lists config entries (with Application/Environment
// preloaded) across every service/environment, for the admin config list page.
func (s *ConfigService) ListAllConfigEntries(ctx context.Context, filter model.ListConfigEntriesFilter) ([]model.ConfigEntry, error) {
	return s.configs.List(ctx, filter)
}

// UpdateConfigEntryInput bundles UpdateConfigEntry's mutable fields.
type UpdateConfigEntryInput struct {
	ApplicationID uuid.UUID
	EnvironmentID uuid.UUID
	Key           string
	Value         string
	ConfigType    string
	IsSecret      bool
	ChangedBy     string
}

// UpdateConfigEntry mutates a ConfigEntry's fields directly by ID (rather
// than upserting by service/environment/key scope, like UpsertConfig does),
// mirroring config_edit's direct-field-update behavior. Returns nil if not found.
func (s *ConfigService) UpdateConfigEntry(ctx context.Context, id uuid.UUID, in UpdateConfigEntryInput) (*model.ConfigEntry, error) {
	entry, err := s.configs.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}

	encryptedValue, err := s.encryption.EncryptForStorage(in.Value)
	if err != nil {
		return nil, err
	}

	entry.ApplicationID = in.ApplicationID
	entry.EnvironmentID = in.EnvironmentID
	entry.Key = in.Key
	entry.Type = in.ConfigType
	entry.IsSecret = in.IsSecret
	entry.Value = encryptedValue
	if err := s.configs.Update(ctx, entry); err != nil {
		return nil, err
	}

	if _, err := s.RecordConfigVersion(ctx, entry, model.ActionUpdate, in.ChangedBy); err != nil {
		return nil, err
	}

	app, err := s.apps.FindByID(ctx, entry.ApplicationID)
	if err != nil {
		return nil, err
	}
	env, err := s.envs.FindByID(ctx, entry.EnvironmentID)
	if err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, app.Name, env.Name); err != nil {
		return nil, err
	}

	entry.Application = *app
	entry.Environment = *env
	return entry, nil
}

// ListServices lists all unique application names used by configs.
func (s *ConfigService) ListServices(ctx context.Context) ([]string, error) {
	return s.apps.ListDistinctNames(ctx)
}

// ListEnvironments lists all environments for a service/application.
func (s *ConfigService) ListEnvironments(ctx context.Context, service string) ([]string, error) {
	app, err := s.apps.FindByName(ctx, service)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	envs, err := s.envs.ListByApplicationID(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(envs))
	for i, env := range envs {
		names[i] = env.Name
	}
	return names, nil
}
