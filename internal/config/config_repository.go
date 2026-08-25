package config

import (
	"context"
	"errors"
	"hash/fnv"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UpsertEntryParams bundles UpsertEntryAndRecordVersion's inputs.
type UpsertEntryParams struct {
	Service        string
	Environment    string
	Key            string
	EncryptedValue string
	ConfigType     string
	IsSecret       bool
	HistoryAction  string // defaults to "create"/"update" based on whether the entry existed
	ChangedBy      string
}

// ConfigRepository is the persistence boundary for ConfigEntry/ConfigEntryVersion
// rows. Not-found lookups return gorm's raw error (including
// gorm.ErrRecordNotFound) rather than swallowing it - callers decide how to
// translate that.
type ConfigRepository interface {
	FindByScopeAndKey(ctx context.Context, applicationID, environmentID uuid.UUID, key string) (*ConfigEntry, error)
	FindByScopeAndKeyWithScope(ctx context.Context, applicationID, environmentID uuid.UUID, key string) (*ConfigEntry, error)
	ListByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]ConfigEntry, error)
	FindByID(ctx context.Context, id uuid.UUID) (*ConfigEntry, error)
	FindByIDWithScope(ctx context.Context, id uuid.UUID) (*ConfigEntry, error)
	List(ctx context.Context, filter ListConfigEntriesFilter) ([]ConfigEntry, error)
	Update(ctx context.Context, entry *ConfigEntry) error
	Delete(ctx context.Context, entry *ConfigEntry) error

	// RecordVersion snapshots entry's current (encrypted) value into history,
	// serialized by a per-entry advisory lock so concurrent writers can't
	// collide on the next version number.
	RecordVersion(ctx context.Context, entry *ConfigEntry, action, changedBy string) (*ConfigEntryVersion, error)

	// UpsertEntryAndRecordVersion is the one repository method that
	// deliberately reaches across aggregates: it creates the Application/
	// Environment rows directly (rather than going through
	// ApplicationRepository/EnvironmentRepository) so the get-or-create and
	// the entry write happen atomically under the same advisory lock and
	// transaction.
	UpsertEntryAndRecordVersion(ctx context.Context, params UpsertEntryParams) (entry *ConfigEntry, action string, err error)

	ListVersions(ctx context.Context, configEntryID uuid.UUID) ([]ConfigEntryVersion, error)
	FindVersion(ctx context.Context, configEntryID uuid.UUID, version int) (*ConfigEntryVersion, error)
}

type gormConfigRepository struct {
	db *gorm.DB
}

func NewConfigRepository(db *gorm.DB) *gormConfigRepository {
	return &gormConfigRepository{db: db}
}

func (r *gormConfigRepository) FindByScopeAndKey(ctx context.Context, applicationID, environmentID uuid.UUID, key string) (*ConfigEntry, error) {
	var entry ConfigEntry
	err := r.db.WithContext(ctx).
		Where("application_id = ? AND environment_id = ? AND key = ?", applicationID, environmentID, key).
		First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *gormConfigRepository) FindByScopeAndKeyWithScope(ctx context.Context, applicationID, environmentID uuid.UUID, key string) (*ConfigEntry, error) {
	var entry ConfigEntry
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Where("application_id = ? AND environment_id = ? AND key = ?", applicationID, environmentID, key).
		First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *gormConfigRepository) ListByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]ConfigEntry, error) {
	var entries []ConfigEntry
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Where("application_id = ? AND environment_id = ?", applicationID, environmentID).
		Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *gormConfigRepository) FindByID(ctx context.Context, id uuid.UUID) (*ConfigEntry, error) {
	var entry ConfigEntry
	if err := r.db.WithContext(ctx).First(&entry, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *gormConfigRepository) FindByIDWithScope(ctx context.Context, id uuid.UUID) (*ConfigEntry, error) {
	var entry ConfigEntry
	err := r.db.WithContext(ctx).Preload("Application").Preload("Environment").First(&entry, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *gormConfigRepository) List(ctx context.Context, filter ListConfigEntriesFilter) ([]ConfigEntry, error) {
	query := r.db.WithContext(ctx).Preload("Application").Preload("Environment").
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

func (r *gormConfigRepository) Update(ctx context.Context, entry *ConfigEntry) error {
	return r.db.WithContext(ctx).Save(entry).Error
}

func (r *gormConfigRepository) Delete(ctx context.Context, entry *ConfigEntry) error {
	return r.db.WithContext(ctx).Delete(entry).Error
}

func (r *gormConfigRepository) RecordVersion(ctx context.Context, entry *ConfigEntry, action, changedBy string) (*ConfigEntryVersion, error) {
	var version *ConfigEntryVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		v, err := recordConfigVersionTx(tx, entry, action, changedBy)
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

func (r *gormConfigRepository) UpsertEntryAndRecordVersion(ctx context.Context, params UpsertEntryParams) (*ConfigEntry, string, error) {
	var entry ConfigEntry
	var historyAction string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize concurrent upserts to the same (service, environment,
		// key) so two writers can't both pass the "not found" check and race
		// on the entry's unique constraint, or on the version number
		// assigned in recordConfigVersionTx below.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey("config-upsert", params.Service, params.Environment, params.Key)).Error; err != nil {
			return err
		}

		app, env, err := getOrCreateScopeTx(tx, params.Service, params.Environment)
		if err != nil {
			return err
		}

		found := ConfigEntry{}
		lookupErr := tx.Where("application_id = ? AND environment_id = ? AND key = ?", app.ID, env.ID, params.Key).First(&found).Error
		created := false
		switch {
		case errors.Is(lookupErr, gorm.ErrRecordNotFound):
			created = true
			found = ConfigEntry{
				ApplicationID: app.ID,
				EnvironmentID: env.ID,
				Key:           params.Key,
				Value:         params.EncryptedValue,
				IsSecret:      params.IsSecret,
				Type:          params.ConfigType,
			}
			if err := tx.Create(&found).Error; err != nil {
				return err
			}
		case lookupErr != nil:
			return lookupErr
		default:
			found.Value = params.EncryptedValue
			found.IsSecret = params.IsSecret
			found.Type = params.ConfigType
			if err := tx.Save(&found).Error; err != nil {
				return err
			}
		}

		historyAction = params.HistoryAction
		if historyAction == "" {
			if created {
				historyAction = ActionCreate
			} else {
				historyAction = ActionUpdate
			}
		}
		if _, err := recordConfigVersionTx(tx, &found, historyAction, params.ChangedBy); err != nil {
			return err
		}

		entry = found
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return &entry, historyAction, nil
}

func (r *gormConfigRepository) ListVersions(ctx context.Context, configEntryID uuid.UUID) ([]ConfigEntryVersion, error) {
	var versions []ConfigEntryVersion
	err := r.db.WithContext(ctx).Where("config_entry_id = ?", configEntryID).Order("version DESC").Find(&versions).Error
	if err != nil {
		return nil, err
	}
	return versions, nil
}

func (r *gormConfigRepository) FindVersion(ctx context.Context, configEntryID uuid.UUID, version int) (*ConfigEntryVersion, error) {
	var target ConfigEntryVersion
	err := r.db.WithContext(ctx).Where("config_entry_id = ? AND version = ?", configEntryID, version).First(&target).Error
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// getOrCreateScopeTx finds or creates the Application/Environment scope for
// a (service, environment) pair within tx. It is only called from
// UpsertEntryAndRecordVersion, under that method's advisory lock - see the
// ConfigRepository doc comment above for why this reaches across aggregates
// instead of going through ApplicationRepository/EnvironmentRepository.
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

// recordConfigVersionTx does the actual "read last version, insert next"
// work within tx, serialized by a per-entry Postgres advisory lock so two
// concurrent writers to the same config can't both compute the same "next
// version" number and collide on the (config_entry_id, version) unique
// constraint.
func recordConfigVersionTx(tx *gorm.DB, entry *ConfigEntry, action, changedBy string) (*ConfigEntryVersion, error) {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey("config-entry-version", entry.ID.String())).Error; err != nil {
		return nil, err
	}

	var last ConfigEntryVersion
	err := tx.Where("config_entry_id = ?", entry.ID).Order("version DESC").First(&last).Error
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
		ConfigEntryID: entry.ID,
		Value:         entry.Value,
		Type:          entry.Type,
		IsSecret:      entry.IsSecret,
		Action:        action,
		Version:       versionNumber,
		ChangedBy:     changedByPtr,
	}
	if err := tx.Create(version).Error; err != nil {
		return nil, err
	}
	return version, nil
}
