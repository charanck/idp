package model

import (
	"context"

	"github.com/google/uuid"
)

// ListConfigEntriesFilter filters ConfigRepository.List. ApplicationIDs
// scopes results to a group-based Application allow-list (nil/empty =
// unrestricted); it composes with ApplicationID, which narrows to one
// specific Application within that scope.
type ListConfigEntriesFilter struct {
	ApplicationID  *uuid.UUID
	ApplicationIDs []uuid.UUID
	EnvironmentID  *uuid.UUID
	IsSecret       *bool
	Query          string
}

// UpsertEntryParams bundles ConfigRepository.UpsertEntryAndRecordVersion's inputs.
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
