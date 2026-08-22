// Package config contains the Application/Environment/ConfigEntry/FeatureFlag/
// Activity models and services, mirroring config_management/models.py and
// services.py.
package config

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Application struct {
	ID        uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	Name      string    `gorm:"column:name"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Application) TableName() string { return "applications" }

func (a *Application) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type Environment struct {
	ID            uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	ApplicationID uuid.UUID `gorm:"column:application_id"`
	Name          string    `gorm:"column:name"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`

	Application Application `gorm:"foreignKey:ApplicationID"`
}

func (Environment) TableName() string { return "environments" }

func (e *Environment) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

const (
	TypeBoolean = "boolean"
	TypeString  = "string"
	TypeNumber  = "number"
	TypeObject  = "object"
	TypeArray   = "array"
)

type ConfigEntry struct {
	ID            uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	ApplicationID uuid.UUID `gorm:"column:application_id"`
	EnvironmentID uuid.UUID `gorm:"column:environment_id"`
	Key           string    `gorm:"column:key"`
	Value         string    `gorm:"column:value"`
	Type          string    `gorm:"column:type"`
	IsSecret      bool      `gorm:"column:is_secret"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`

	Application Application `gorm:"foreignKey:ApplicationID"`
	Environment Environment `gorm:"foreignKey:EnvironmentID"`
}

func (ConfigEntry) TableName() string { return "config_entries" }

func (c *ConfigEntry) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

const (
	ActionCreate   = "create"
	ActionUpdate   = "update"
	ActionRollback = "rollback"
)

type ConfigEntryVersion struct {
	ID            uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	ConfigEntryID uuid.UUID `gorm:"column:config_entry_id"`
	Value         string    `gorm:"column:value"`
	Type          string    `gorm:"column:type"`
	IsSecret      bool      `gorm:"column:is_secret"`
	Action        string    `gorm:"column:action"`
	Version       int       `gorm:"column:version"`
	ChangedBy     *string   `gorm:"column:changed_by"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ConfigEntryVersion) TableName() string { return "config_entry_versions" }

func (v *ConfigEntryVersion) BeforeCreate(tx *gorm.DB) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	return nil
}

type FeatureFlag struct {
	ID            uuid.UUID  `gorm:"column:id;type:uuid;primaryKey"`
	ApplicationID uuid.UUID  `gorm:"column:application_id"`
	EnvironmentID uuid.UUID  `gorm:"column:environment_id"`
	Name          string     `gorm:"column:name"`
	Description   *string    `gorm:"column:description"`
	IsEnabled     bool       `gorm:"column:is_enabled"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt     *time.Time `gorm:"column:deleted_at"`

	Application Application `gorm:"foreignKey:ApplicationID"`
	Environment Environment `gorm:"foreignKey:EnvironmentID"`
}

func (FeatureFlag) TableName() string { return "feature_flags" }

func (f *FeatureFlag) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}

const (
	ActivityTypeCreate      = "create"
	ActivityTypeUpdate      = "update"
	ActivityTypeDelete      = "delete"
	ActivityTypeRead        = "read"
	ActivityTypeToggle      = "toggle"
	ActivityTypeLogin       = "login"
	ActivityTypeLogout      = "logout"
	ActivityTypeLoginFailed = "login_failed"
	ActivityTypeAuthFailed  = "auth_failed"
)

type Activity struct {
	ID           uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	Type         string    `gorm:"column:type"`
	Resource     string    `gorm:"column:resource"`
	ResourceID   string    `gorm:"column:resource_id"`
	ResourceName *string   `gorm:"column:resource_name"`
	UserEmail    *string   `gorm:"column:user_email"`
	Details      *string   `gorm:"column:details"`
	IPAddress    *string   `gorm:"column:ip_address"`
	Timestamp    time.Time `gorm:"column:timestamp;autoCreateTime"`
}

func (Activity) TableName() string { return "activities" }

func (a *Activity) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
