package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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

func getOrCreateScopeTx(tx *gorm.DB, service, environment string) (*Application, *Environment, error) {
	var app Application
	if err := tx.Where("name = ?", service).First(&app).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		app = Application{Name: service}
		if err := tx.Create(&app).Error; err != nil {
			return nil, nil, err
		}
	}

	var env Environment
	err := tx.Where("application_id = ? AND name = ?", app.ID, environment).First(&env).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		env = Environment{ApplicationID: app.ID, Name: environment}
		if err := tx.Create(&env).Error; err != nil {
			return nil, nil, err
		}
	}

	return &app, &env, nil
}

// advisoryLockKey hashes an arbitrary identity into a Postgres advisory-lock
// key (bigint), used to serialize concurrent writers targeting the same
// logical row without taking a table-wide lock.
func advisoryLockKey(parts ...string) int64 {
	h := fnv.New64a()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return int64(h.Sum64())
}

// getScope looks up an existing Application/Environment without creating
// them; either return value is nil if not found.
func (s *ConfigService) getScope(ctx context.Context, service, environment string) (*Application, *Environment, error) {
	db := s.db.WithContext(ctx)

	var app Application
	err := db.Where("name = ?", service).First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var env Environment
	err = db.Where("application_id = ? AND name = ?", app.ID, environment).First(&env).Error
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
func (s *ConfigService) RecordConfigVersion(ctx context.Context, config *ConfigEntry, action string, changedBy string) (*ConfigEntryVersion, error) {
	var version *ConfigEntryVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		v, err := recordConfigVersionTx(tx, config, action, changedBy)
		if err != nil {
			return err
		}
		version = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return version, nil
}

// recordConfigVersionTx does the actual "read last version, insert next"
// work within tx, serialized by a per-entry Postgres advisory lock so two
// concurrent writers to the same config can't both compute the same "next
// version" number and collide on the (config_entry_id, version) unique
// constraint.
func recordConfigVersionTx(tx *gorm.DB, config *ConfigEntry, action, changedBy string) (*ConfigEntryVersion, error) {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey("config-entry-version", config.ID.String())).Error; err != nil {
		return nil, err
	}

	var last ConfigEntryVersion
	err := tx.Where("config_entry_id = ?", config.ID).Order("version DESC").First(&last).Error
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
	if err := tx.Create(version).Error; err != nil {
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

	configType := opts.ConfigType
	if configType == "" {
		configType = TypeString
	}

	var entry ConfigEntry
	var historyAction string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize concurrent upserts to the same (service, environment,
		// key) so two writers can't both pass the "not found" check and race
		// on the entry's unique constraint, or on the version number
		// assigned in recordConfigVersionTx below.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey("config-upsert", service, environment, key)).Error; err != nil {
			return err
		}

		app, env, err := getOrCreateScopeTx(tx, service, environment)
		if err != nil {
			return err
		}

		found := ConfigEntry{}
		lookupErr := tx.Where("application_id = ? AND environment_id = ? AND key = ?", app.ID, env.ID, key).First(&found).Error
		created := false
		switch {
		case errors.Is(lookupErr, gorm.ErrRecordNotFound):
			created = true
			found = ConfigEntry{
				ApplicationID: app.ID,
				EnvironmentID: env.ID,
				Key:           key,
				Value:         encryptedValue,
				IsSecret:      opts.IsSecret,
				Type:          configType,
			}
			if err := tx.Create(&found).Error; err != nil {
				return err
			}
		case lookupErr != nil:
			return lookupErr
		default:
			found.Value = encryptedValue
			found.IsSecret = opts.IsSecret
			found.Type = configType
			if err := tx.Save(&found).Error; err != nil {
				return err
			}
		}

		historyAction = opts.HistoryAction
		if historyAction == "" {
			if created {
				historyAction = ActionCreate
			} else {
				historyAction = ActionUpdate
			}
		}
		if _, err := recordConfigVersionTx(tx, &found, historyAction, opts.ChangedBy); err != nil {
			return err
		}

		entry = found
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := s.InvalidateScopeCache(ctx, service, environment); err != nil {
		return nil, err
	}

	slog.Info("upserted config", "action", historyAction, "service", service, "environment", environment, "key", key, "secret", opts.IsSecret)
	return &entry, nil
}

// GetConfig returns a specific configuration or secret, or nil if not found.
func (s *ConfigService) GetConfig(ctx context.Context, service, environment, key string) (*ConfigEntry, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return nil, nil
	}

	var entry ConfigEntry
	err = s.db.WithContext(ctx).Where("application_id = ? AND environment_id = ? AND key = ?", app.ID, env.ID, key).First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetConfigWithScope returns a specific config entry with its Application/Environment preloaded.
func (s *ConfigService) GetConfigWithScope(ctx context.Context, service, environment, key string) (*ConfigEntry, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return nil, nil
	}

	var entry ConfigEntry
	err = s.db.WithContext(ctx).Preload("Application").Preload("Environment").
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
func (s *ConfigService) ListConfigs(ctx context.Context, service, environment string) ([]ConfigEntry, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	if app == nil || env == nil {
		return []ConfigEntry{}, nil
	}

	var entries []ConfigEntry
	err = s.db.WithContext(ctx).Preload("Application").Preload("Environment").
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
	err = s.db.WithContext(ctx).Preload("Application").Preload("Environment").First(&entry, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("config delete failed: not found", "id", configID)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
		return false, err
	}
	if err := s.InvalidateScopeCache(ctx, entry.Application.Name, entry.Environment.Name); err != nil {
		return false, err
	}
	slog.Info("deleted config", "service", entry.Application.Name, "environment", entry.Environment.Name, "key", entry.Key)
	return true, nil
}

// GetConfigHistory lists version history for a config entry, newest first.
func (s *ConfigService) GetConfigHistory(ctx context.Context, configID string) ([]ConfigEntryVersion, error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return []ConfigEntryVersion{}, nil
	}

	db := s.db.WithContext(ctx)

	var entry ConfigEntry
	if err := db.First(&entry, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []ConfigEntryVersion{}, nil
		}
		return nil, err
	}

	var versions []ConfigEntryVersion
	if err := db.Where("config_entry_id = ?", id).Order("version DESC").Find(&versions).Error; err != nil {
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
	err = s.db.WithContext(ctx).Preload("Application").Preload("Environment").First(&entry, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var target ConfigEntryVersion
	err = s.db.WithContext(ctx).Where("config_entry_id = ? AND version = ?", entry.ID, version).First(&target).Error
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
func (s *ConfigService) GetConfigByID(ctx context.Context, id uuid.UUID) (*ConfigEntry, error) {
	var entry ConfigEntry
	err := s.db.WithContext(ctx).Preload("Application").Preload("Environment").First(&entry, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// ListConfigEntriesFilter filters ListAllConfigEntries.
type ListConfigEntriesFilter struct {
	ApplicationID *uuid.UUID
	EnvironmentID *uuid.UUID
	IsSecret      *bool
	Query         string
}

// ListAllConfigEntries lists config entries (with Application/Environment
// preloaded) across every service/environment, for the admin config list page.
func (s *ConfigService) ListAllConfigEntries(ctx context.Context, filter ListConfigEntriesFilter) ([]ConfigEntry, error) {
	query := s.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Joins("JOIN applications ON applications.id = config_entries.application_id").
		Order("applications.name, config_entries.key, config_entries.is_secret, config_entries.type")
	if filter.ApplicationID != nil {
		query = query.Where("config_entries.application_id = ?", *filter.ApplicationID)
	}
	if filter.EnvironmentID != nil {
		query = query.Where("config_entries.environment_id = ?", *filter.EnvironmentID)
	}
	if filter.IsSecret != nil {
		query = query.Where("config_entries.is_secret = ?", *filter.IsSecret)
	}
	if filter.Query != "" {
		query = query.Where("config_entries.key ILIKE ?", "%"+filter.Query+"%")
	}
	var entries []ConfigEntry
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
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
func (s *ConfigService) UpdateConfigEntry(ctx context.Context, id uuid.UUID, in UpdateConfigEntryInput) (*ConfigEntry, error) {
	var entry ConfigEntry
	err := s.db.WithContext(ctx).First(&entry, "id = ?", id).Error
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
	if err := s.db.WithContext(ctx).Save(&entry).Error; err != nil {
		return nil, err
	}

	if _, err := s.RecordConfigVersion(ctx, &entry, ActionUpdate, in.ChangedBy); err != nil {
		return nil, err
	}

	var app Application
	if err := s.db.WithContext(ctx).First(&app, "id = ?", entry.ApplicationID).Error; err != nil {
		return nil, err
	}
	var env Environment
	if err := s.db.WithContext(ctx).First(&env, "id = ?", entry.EnvironmentID).Error; err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, app.Name, env.Name); err != nil {
		return nil, err
	}

	entry.Application = app
	entry.Environment = env
	return &entry, nil
}

// ListServices lists all unique application names used by configs.
func (s *ConfigService) ListServices(ctx context.Context) ([]string, error) {
	var names []string
	err := s.db.WithContext(ctx).Model(&Application{}).Distinct().Order("name").Pluck("name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}

// ListEnvironments lists all environments for a service/application.
func (s *ConfigService) ListEnvironments(ctx context.Context, service string) ([]string, error) {
	db := s.db.WithContext(ctx)

	var app Application
	err := db.Where("name = ?", service).First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	var names []string
	err = db.Model(&Environment{}).Where("application_id = ?", app.ID).Distinct().Order("name").Pluck("name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}
