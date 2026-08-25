// Command server runs the control-plane HTTP server: the Ninja-equivalent
// JSON API under /api/v1/... and the session-authenticated web UI at /,
// mirroring control_plane_project/settings.py + urls.py + manage.py runserver.
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"controlplane/internal/api"
	"controlplane/internal/appconfig"
	"controlplane/internal/auth"
	"controlplane/internal/notification"
	"controlplane/internal/webui"
)

// version is the service.version resource attribute reported to OTEL,
// overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx := context.Background()

	otelShutdown, err := setupObservability(ctx, version)
	if err != nil {
		log.Fatalf("setup observability: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			slog.Error("otel shutdown", "err", err)
		}
	}()

	cfg, err := appconfig.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	gdb, err := newDatabase(cfg)
	if err != nil {
		log.Fatalf("%v", err)
	}

	rdb, err := newRedisClient(cfg)
	if err != nil {
		log.Fatalf("%v", err)
	}

	svc := newCoreServices(gdb, rdb, cfg)
	notif := newNotificationStack(gdb, rdb, svc.Encryption)

	if err := bootstrapAdmin(gdb, cfg); err != nil {
		log.Fatalf("bootstrap admin user: %v", err)
	}

	e := newEchoServer(svc.Sessions)

	webuiAuthMW := webui.NewAuthMiddleware(svc.Auth)
	// svc.Config satisfies webui.ApplicationStore, webui.EnvironmentStore, and webui.ConfigStore
	// all at once, so several constructors below take it more than once, positionally, for
	// different parameters - the compiler can't catch a swapped argument order here since every
	// position accepts the same concrete type. Double-check argument order against each
	// constructor's signature when editing this block.
	webuiHandlers := &webui.Handlers{
		Dashboard:            webui.NewDashboardHandler(svc.Dashboard),
		Activity:             webui.NewActivityHandler(svc.Activity),
		Application:          webui.NewApplicationHandler(svc.Config, svc.Activity),
		Environment:          webui.NewEnvironmentHandler(svc.Config, svc.Config, svc.Activity),
		Config:               webui.NewConfigHandler(svc.Config, svc.Config, svc.Config, svc.Activity),
		Flag:                 webui.NewFlagHandler(svc.Flags, svc.Config, svc.Config, svc.Activity),
		Client:               webui.NewClientHandler(svc.Auth, svc.Activity),
		User:                 webui.NewUserHandler(svc.Auth, svc.Activity),
		Auth:                 webui.NewAuthHandler(svc.Auth, svc.OAuth, svc.RateLimiter, svc.Activity, cfg.AuthRateLimit, cfg.AuthRateLimitWindowSeconds),
		OAuthLogin:           webui.NewOAuthLoginHandler(svc.OAuth, svc.Activity),
		OAuthProvider:        webui.NewOAuthProviderHandler(svc.OAuth, svc.Activity),
		NotificationSettings: webui.NewNotificationSettingsHandler(notif.Settings, svc.Activity),
	}
	webui.RegisterRoutes(e, webuiHandlers, webuiAuthMW)

	apiKeyAuthMW := api.NewAPIKeyAuthMiddleware(svc.Auth, svc.RateLimiter, cfg.AuthRateLimitWindowSeconds, cfg.S2SAuthRateLimit)
	apiGroup := e.Group("/api/v1")
	api.RegisterConfigRoutes(apiGroup.Group("/config"), api.NewConfigHandler(svc.Config), api.NewFeatureFlagHandler(svc.Flags), apiKeyAuthMW)

	notifAuthMW := notification.NewAPIKeyAuthMiddleware(
		auth.ServiceClientAuthenticator{Service: svc.Auth},
		svc.RateLimiter,
		cfg.AuthRateLimitWindowSeconds,
		cfg.S2SAuthRateLimit,
	)
	notification.RegisterRoutes(
		apiGroup.Group("/notifications"),
		notification.NewNotificationHandler(notif.Service, notif.Service, notif.Service, notif.Channels),
		notification.NewSessionHandler(notif.TokenIssuer),
		notification.NewSSEHandler(notif.TokenIssuer, notif.Hub),
		notification.NewInAppHandler(notif.TokenIssuer, notif.Service),
		notifAuthMW,
	)

	go func() {
		if err := notif.Run(); err != nil {
			slog.Error("asynq server stopped", "err", err)
		}
	}()

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := e.Start(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	notif.Shutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown", "err", err)
	}
}
