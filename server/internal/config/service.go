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
)

// ConfigService mirrors config_management/services.py's ConfigService.
type ConfigService struct {
	db           *gorm.DB
	encryption   *crypto.EncryptionService
	cache        cache.Cache
	cacheTimeout time.Duration
}

func NewConfigService(db *gorm.DB, encryption *crypto.EncryptionService, c cache.Cache, cacheTimeout time.Duration) *ConfigService {
	return &ConfigService{db: db, encryption: encryption, cache: c, cacheTimeout: cacheTimeout}
}

func (s *ConfigService) getOrCreateScope(service, environment string) (*Application, *Environment, error) {
	var app Application
	if err := s.db.Where("name = ?", service).First(&app).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		app = Application{Name: service}
		if err := s.db.Create(&app).Error; err != nil {
			return nil, nil, err
		}
	}

	var env Environment
	err := s.db.Where("application_id = ? AND name = ?", app.ID, environment).First(&env).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		env = Environment{ApplicationID: app.ID, Name: environment}
		if err := s.db.Create(&env).Error; err != nil {
			return nil, nil, err
		}
	}

	return &app, &env, nil
}

// getScope looks up an existing Application/Environment without creating
// them; either return value is nil if not found.
func (s *ConfigService) getScope(service, environment string) (*Application, *Environment, error) {
	var app Application
	err := s.db.Where("name = ?", service).First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var env Environment
	err = s.db.Where("application_id = ? AND name = ?", app.ID, environment).First(&env).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &app, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &app, &env, nil
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
func (s *ConfigService) RecordConfigVersion(config *ConfigEntry, action string, changedBy string) (*ConfigEntryVersion, error) {
	var last ConfigEntryVersion
	err := s.db.Where("config_entry_id = ?", config.ID).Order("version DESC").First(&last).Error
	versionNumber := 1
	if err == nil {
		versionNumber = last.Version + 1
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var changedByPtr *string
	if changedBy != "" {
		changedByPtr = &changedBy
	}

	version := &ConfigEntryVersion{
		ConfigEntryID: config.ID,
		Value:         config.Value,
		Type:          config.Type,
		IsSecret:      config.IsSecret,
		Action:        action,
		Version:       versionNumber,
		ChangedBy:     changedByPtr,
	}
	if err := s.db.Create(version).Error; err != nil {
		return nil, err
	}
	return version, nil
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
func (s *ConfigService) UpsertConfig(ctx context.Context, service, environment, key, value string, opts UpsertOptions) (*ConfigEntry, error) {
	encryptedValue, err := s.encryption.EncryptForStorage(value)
	if err != nil {
		return nil, err
	}

	app, env, err := s.getOrCreateScope(service, environment)
	if err != nil {
		return nil, err
	}

	configType := opts.ConfigType
	if configType == "" {
		configType = TypeString
	}

	var entry ConfigEntry
	err = s.db.Where("application_id = ? AND environment_id = ? AND key = ?", app.ID, env.ID, key).First(&entry).Error
	created := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		created = true
		entry = ConfigEntry{
			ApplicationID: app.ID,
			EnvironmentID: env.ID,
			Key:           key,
			Value:         encryptedValue,
			IsSecret:      opts.IsSecret,
			Type:          configType,
		}
		if err := s.db.Create(&entry).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		entry.Value = encryptedValue
		entry.IsSecret = opts.IsSecret
		entry.Type = configType
		if err := s.db.Save(&entry).Error; err != nil {
			return nil, err
		}
	}

	historyAction := opts.HistoryAction
	if historyAction == "" {
		if created {
			historyAction = ActionCreate
		} else {
			historyAction = ActionUpdate
		}
	}
	if _, err := s.RecordConfigVersion(&entry, historyAction, opts.ChangedBy); err != nil {
		return nil, err
	}

	if err := s.InvalidateScopeCache(ctx, service, environment); err != nil {
		return nil, err
	}

	slog.Info("upserted config", "action", historyAction, "service", service, "environment", environment, "key", key, "secret", opts.IsSecret)
	return &entry, nil
}

// GetConfig returns a specific configuration or secret, or nil if not found.
func (s *ConfigService) GetConfig(service, environment, key string) (*ConfigEntry, error) {
	app, env, err := s.getScope(service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return nil, nil
	}

	var entry ConfigEntry
	err = s.db.Where("application_id = ? AND environment_id = ? AND key = ?", app.ID, env.ID, key).First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetConfigWithScope returns a specific config entry with its Application/Environment preloaded.
func (s *ConfigService) GetConfigWithScope(service, environment, key string) (*ConfigEntry, error) {
	app, env, err := s.getScope(service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return nil, nil
	}

	var entry ConfigEntry
	err = s.db.Preload("Application").Preload("Environment").
		Where("application_id = ? AND environment_id = ? AND key = ?", app.ID, env.ID, key).
		First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// ListConfigs lists all configurations for a service/environment.
func (s *ConfigService) ListConfigs(service, environment string) ([]ConfigEntry, error) {
	app, env, err := s.getScope(service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return []ConfigEntry{}, nil
	}

	var entries []ConfigEntry
	err = s.db.Preload("Application").Preload("Environment").
		Where("application_id = ? AND environment_id = ?", app.ID, env.ID).
		Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// DecryptConfigValue decrypts a config value for internal use.
func (s *ConfigService) DecryptConfigValue(entry *ConfigEntry) (string, error) {
	return s.encryption.DecryptFromStorage(entry.Value)
}

// DecryptConfigValueOrOriginal decrypts a value when possible, falling back
// to the stored (legacy plaintext) value on an invalid token.
func (s *ConfigService) DecryptConfigValueOrOriginal(entry *ConfigEntry) string {
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

	var entry ConfigEntry
	err = s.db.Preload("Application").Preload("Environment").First(&entry, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("config delete failed: not found", "id", configID)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := s.db.Delete(&entry).Error; err != nil {
		return false, err
	}
	if err := s.InvalidateScopeCache(ctx, entry.Application.Name, entry.Environment.Name); err != nil {
		return false, err
	}
	slog.Info("deleted config", "service", entry.Application.Name, "environment", entry.Environment.Name, "key", entry.Key)
	return true, nil
}

// GetConfigHistory lists version history for a config entry, newest first.
func (s *ConfigService) GetConfigHistory(configID string) ([]ConfigEntryVersion, error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return []ConfigEntryVersion{}, nil
	}

	var entry ConfigEntry
	if err := s.db.First(&entry, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []ConfigEntryVersion{}, nil
		}
		return nil, err
	}

	var versions []ConfigEntryVersion
	if err := s.db.Where("config_entry_id = ?", id).Order("version DESC").Find(&versions).Error; err != nil {
		return nil, err
	}
	return versions, nil
}

// DecryptVersionValue decrypts a historical version's value for internal use.
func (s *ConfigService) DecryptVersionValue(version *ConfigEntryVersion) (string, error) {
	return s.encryption.DecryptFromStorage(version.Value)
}

// RollbackConfig restores a prior version by writing it again via
// UpsertConfig, so the rollback itself becomes a new, auditable version.
func (s *ConfigService) RollbackConfig(ctx context.Context, configID string, version int, changedBy string) (*ConfigEntry, error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return nil, nil //nolint:nilnil // malformed/not-found id is a valid "not found" outcome, matching the Python service.
	}

	var entry ConfigEntry
	err = s.db.Preload("Application").Preload("Environment").First(&entry, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var target ConfigEntryVersion
	err = s.db.Where("config_entry_id = ? AND version = ?", entry.ID, version).First(&target).Error
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
		HistoryAction: ActionRollback,
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
func (s *ConfigService) GetConfigForClient(service, environment, key, clientEncryptionKey string) (*ClientConfig, error) {
	entry, err := s.GetConfigWithScope(service, environment, key)
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

	entries, err := s.ListConfigs(service, environment)
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

// ListServices lists all unique application names used by configs.
func (s *ConfigService) ListServices() ([]string, error) {
	var names []string
	err := s.db.Model(&Application{}).Distinct().Order("name").Pluck("name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}

// ListEnvironments lists all environments for a service/application.
func (s *ConfigService) ListEnvironments(service string) ([]string, error) {
	var app Application
	err := s.db.Where("name = ?", service).First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	var names []string
	err = s.db.Model(&Environment{}).Where("application_id = ?", app.ID).Distinct().Order("name").Pluck("name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}
