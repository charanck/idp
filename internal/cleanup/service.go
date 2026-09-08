// Package cleanup implements a monthly DBOS-scheduled workflow that prunes
// data that would otherwise grow unbounded: delivered/failed notifications,
// the append-only activity audit log, and expired (single-use) OIDC
// authorization codes.
package cleanup

import (
	"context"
	"time"
)

// Retention windows are kept as named constants rather than env-configurable
// settings - changing how long audit/notification history is kept is a
// deliberate, reviewed decision, not something that should be flippable at
// deploy time.
const (
	NotificationRetention = 90 * 24 * time.Hour
	ActivityRetention     = 180 * 24 * time.Hour
)

// NotificationPruner deletes notification records older than a cutoff.
type NotificationPruner interface {
	DeleteOlderThan(ctx context.Context, before time.Time) error
}

// ActivityPruner deletes activity log entries older than a cutoff.
type ActivityPruner interface {
	DeleteOlderThan(ctx context.Context, before time.Time) error
}

// AuthCodePruner deletes OIDC authorization codes past their expiry.
type AuthCodePruner interface {
	DeleteExpired(ctx context.Context, before time.Time) error
}

// Service prunes each data set in turn against a single reference time, so a
// run is reproducible given the same input regardless of when it actually
// executes.
type Service struct {
	notifications NotificationPruner
	activities    ActivityPruner
	authCodes     AuthCodePruner
}

func NewService(notifications NotificationPruner, activities ActivityPruner, authCodes AuthCodePruner) *Service {
	return &Service{notifications: notifications, activities: activities, authCodes: authCodes}
}

// Run hard-deletes notifications older than NotificationRetention, activity
// log entries older than ActivityRetention, and OIDC authorization codes
// past their expiry, all relative to now.
func (s *Service) Run(ctx context.Context, now time.Time) error {
	if err := s.notifications.DeleteOlderThan(ctx, now.Add(-NotificationRetention)); err != nil {
		return err
	}
	if err := s.activities.DeleteOlderThan(ctx, now.Add(-ActivityRetention)); err != nil {
		return err
	}
	return s.authCodes.DeleteExpired(ctx, now)
}
