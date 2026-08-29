package main

import (
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	configmodel "controlplane/internal/model/config"
	"controlplane/internal/crypto"
	"controlplane/internal/notification"
	configrepo "controlplane/internal/repository/config"
	notificationrepo "controlplane/internal/repository/notification"
)

// notificationStack owns construction of the notification subsystem,
// including registering its DBOS-backed background delivery workflow
// against the server's shared dbos.Context (see cmd/server/dbos.go - DBOS
// bootstrap/lifecycle is not notification-specific). It stays in the same
// binary as the HTTP server, but its wiring is isolated here instead of
// being inlined in main().
type notificationStack struct {
	Settings    *notification.ProviderSettingService
	TokenIssuer *notification.TokenIssuer
	Channels    notification.ChannelRegistry
	Service     *notification.NotificationService
	Hub         *notification.Hub
	Apps        configmodel.ApplicationRepository
}

// newNotificationStack wires the notification subsystem, including
// registering its DBOS workflow/queue against the caller-supplied dbosCtx.
// Construction happens in two phases to break a dependency cycle: the DBOS
// Enqueuer needs a *Worker, and *Worker needs the *NotificationService it's
// later attached to - so the service is built with a nil enqueuer first,
// then service.SetEnqueuer is called once the worker/enqueuer exist. No
// caller can reach CreateNotification before newNotificationStack returns,
// so there's no window where the nil enqueuer is used for real.
func newNotificationStack(dbosCtx dbos.Context, gdb *gorm.DB, rdb *redis.Client, encryption *crypto.EncryptionService) (*notificationStack, error) {
	settings := notification.NewProviderSettingService(notificationrepo.NewProviderSettingRepository(gdb), encryption)
	tokenIssuer := notification.NewTokenIssuer(encryption)
	channels := notification.NewChannelRegistry()
	hub := notification.NewHub(rdb)

	apps := configrepo.NewApplicationRepository(gdb)
	service := notification.NewNotificationService(notificationrepo.NewNotificationRepository(gdb), apps, nil)
	worker := notification.NewWorker(service, settings, channels, hub)

	enqueuer, err := notification.NewTaskEnqueuer(dbosCtx, worker)
	if err != nil {
		return nil, fmt.Errorf("register notification send workflow: %w", err)
	}
	service.SetEnqueuer(enqueuer)

	return &notificationStack{
		Settings:    settings,
		TokenIssuer: tokenIssuer,
		Channels:    channels,
		Service:     service,
		Hub:         hub,
		Apps:        apps,
	}, nil
}
