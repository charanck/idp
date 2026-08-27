// Package model contains the Activity model, mirroring
// common/activity_logger.py's audit log row.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
