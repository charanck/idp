package main

import (
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/crypto"
	"controlplane/internal/notification"
	notificationrepo "controlplane/internal/repository/notification"
)

// notificationStack owns construction and lifecycle of the notification
// subsystem, including the asynq background worker. It stays in the same
// binary as the HTTP server, but its startup/shutdown is isolated here
// instead of being inlined in main().
type notificationStack struct {
	Settings    *notification.ProviderSettingService
	TokenIssuer *notification.TokenIssuer
	Channels    notification.ChannelRegistry
	Service     *notification.NotificationService
	Hub         *notification.Hub

	asynqServer *asynq.Server
	asynqMux    *asynq.ServeMux
}

func newNotificationStack(gdb *gorm.DB, rdb *redis.Client, encryption *crypto.EncryptionService) *notificationStack {
	settings := notification.NewProviderSettingService(notificationrepo.NewProviderSettingRepository(gdb), encryption)
	tokenIssuer := notification.NewTokenIssuer(encryption)
	channels := notification.NewChannelRegistry()
	asynqClient := notification.NewAsynqClient(rdb)
	enqueuer := notification.NewTaskEnqueuer(asynqClient)
	service := notification.NewNotificationService(notificationrepo.NewNotificationRepository(gdb), enqueuer)
	hub := notification.NewHub(rdb)
	worker := notification.NewWorker(service, settings, channels, hub)

	return &notificationStack{
		Settings:    settings,
		TokenIssuer: tokenIssuer,
		Channels:    channels,
		Service:     service,
		Hub:         hub,
		asynqServer: notification.NewAsynqServer(rdb, asynq.Config{}),
		asynqMux:    notification.NewAsynqMux(worker),
	}
}

// Run blocks, processing background tasks until Shutdown is called.
func (n *notificationStack) Run() error {
	return n.asynqServer.Run(n.asynqMux)
}

// Shutdown stops the background worker gracefully.
func (n *notificationStack) Shutdown() {
	n.asynqServer.Shutdown()
}
