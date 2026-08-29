// Package model contains the Snapshot model backing the dashboard's trend
// charts - a rolling window of hourly gauge/event counts, mirroring
// common/analytics's snapshot table.
package model

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Snapshot is one hourly analytics data point: gauge counts (current totals,
// same shape as dashboard.Counts) plus event counts observed in the hour
// ending at CapturedAt.
type Snapshot struct {
	ID                       uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	CapturedAt               time.Time `gorm:"column:captured_at"`
	ApplicationCount         int64     `gorm:"column:application_count"`
	EnvironmentCount         int64     `gorm:"column:environment_count"`
	ConfigCount              int64     `gorm:"column:config_count"`
	SecretCount              int64     `gorm:"column:secret_count"`
	FlagCount                int64     `gorm:"column:flag_count"`
	ClientCount              int64     `gorm:"column:client_count"`
	ActivityCreateCount      int64     `gorm:"column:activity_create_count"`
	ActivityUpdateCount      int64     `gorm:"column:activity_update_count"`
	ActivityDeleteCount      int64     `gorm:"column:activity_delete_count"`
	ActivityLoginCount       int64     `gorm:"column:activity_login_count"`
	ActivityLoginFailedCount int64     `gorm:"column:activity_login_failed_count"`
	NotificationSentCount    int64     `gorm:"column:notification_sent_count"`
	NotificationFailedCount  int64     `gorm:"column:notification_failed_count"`
	S2SRequestCount          int64     `gorm:"column:s2s_request_count"`
	CreatedAt                time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (Snapshot) TableName() string { return "analytics_snapshots" }

func (s *Snapshot) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// Repository is the persistence seam for analytics snapshots and the
// windowed event counts they're built from.
type Repository interface {
	Create(ctx context.Context, s *Snapshot) error
	// ListSince lists snapshots captured at or after since, oldest first.
	ListSince(ctx context.Context, since time.Time) ([]Snapshot, error)
	// DeleteOlderThan deletes snapshots captured before before, implementing
	// the sliding-window retention.
	DeleteOlderThan(ctx context.Context, before time.Time) error
	// CountActivityByTypeSince counts activity rows of typ created in
	// (since, until].
	CountActivityByTypeSince(ctx context.Context, typ string, since, until time.Time) (int64, error)
	// CountNotificationsByStatusSince counts notifications with status
	// updated in (since, until].
	CountNotificationsByStatusSince(ctx context.Context, status string, since, until time.Time) (int64, error)
}
