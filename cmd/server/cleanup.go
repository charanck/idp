package main

import (
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"gorm.io/gorm"

	"controlplane/internal/cleanup"
	activityrepo "controlplane/internal/repository/activity"
	authrepo "controlplane/internal/repository/auth"
	notificationrepo "controlplane/internal/repository/notification"
)

// newCleanupStack wires the monthly data-retention cleanup subsystem,
// including registering its DBOS-scheduled workflow against the
// caller-supplied dbosCtx. Wiring is isolated here the same way
// newAnalyticsStack isolates the analytics subsystem's wiring.
func newCleanupStack(dbosCtx dbos.Context, gdb *gorm.DB) error {
	service := cleanup.NewService(
		notificationrepo.NewNotificationRepository(gdb),
		activityrepo.NewRepository(gdb),
		authrepo.NewOIDCAuthorizationCodeRepository(gdb),
	)

	if _, err := cleanup.NewScheduler(dbosCtx, service); err != nil {
		return fmt.Errorf("register monthly cleanup workflow: %w", err)
	}

	return nil
}
