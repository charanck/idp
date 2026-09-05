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

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	apihttp "controlplane/api/http"
	"controlplane/internal/appconfig"
	"controlplane/internal/auth"
	"controlplane/internal/notification"
	"controlplane/web"
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

	dbosCtx, err := newDBOSContext(cfg)
	if err != nil {
		log.Fatalf("setup dbos context: %v", err)
	}

	svc := newCoreServices(gdb, rdb, cfg)
	notif, err := newNotificationStack(dbosCtx, gdb, rdb, svc.Encryption)
	if err != nil {
		log.Fatalf("setup notification stack: %v", err)
	}
	an, err := newAnalyticsStack(dbosCtx, gdb, rdb, svc.Dashboard, svc.Cache)
	if err != nil {
		log.Fatalf("setup analytics stack: %v", err)
	}

	if err := bootstrapAdmin(gdb, cfg); err != nil {
		log.Fatalf("bootstrap admin user: %v", err)
	}

	e := newEchoServer(svc.Sessions)

	webAuthMW := web.NewAuthMiddleware(svc.Auth, svc.Auth)
	// svc.Config satisfies web.ApplicationStore, web.EnvironmentStore, and web.ConfigStore
	// all at once, so several constructors below take it more than once, positionally, for
	// different parameters - the compiler can't catch a swapped argument order here since every
	// position accepts the same concrete type. Double-check argument order against each
	// constructor's signature when editing this block.
	webHandlers := &web.Handlers{
		Dashboard:            web.NewDashboardHandler(svc.Dashboard, an.Service),
		Activity:             web.NewActivityHandler(svc.Activity),
		Application:          web.NewApplicationHandler(svc.Config, svc.Activity),
		Environment:          web.NewEnvironmentHandler(svc.Config, svc.Config, svc.Activity),
		Config:               web.NewConfigHandler(svc.Config, svc.Config, svc.Config, svc.Activity),
		Flag:                 web.NewFlagHandler(svc.Flags, svc.Config, svc.Config, svc.Activity),
		Client:               web.NewClientHandler(svc.Auth, svc.Config, svc.Auth, svc.Activity),
		User:                 web.NewUserHandler(svc.Auth, svc.Activity),
		Group:                web.NewGroupHandler(svc.Auth, svc.Config, svc.Activity),
		Policy:               web.NewPolicyHandler(svc.Auth, svc.Activity),
		Auth:                 web.NewAuthHandler(svc.Auth, svc.OAuth, svc.RateLimiter, svc.Activity, cfg.AuthRateLimit, cfg.AuthRateLimitWindowSeconds),
		OAuthLogin:           web.NewOAuthLoginHandler(svc.OAuth, svc.Activity),
		OAuthProvider:        web.NewOAuthProviderHandler(svc.OAuth, svc.Activity),
		OIDC:                 web.NewOIDCHandler(svc.OIDC, svc.Activity),
		NotificationSettings: web.NewNotificationSettingsHandler(notif.Settings, svc.Activity),
		Notification:         web.NewNotificationHandler(notif.Service, svc.Config),
	}
	web.RegisterRoutes(e, webHandlers, webAuthMW)

	apiKeyAuthMW := apihttp.NewAPIKeyAuthMiddleware(svc.Auth, svc.RateLimiter, an.Counter, cfg.AuthRateLimitWindowSeconds, cfg.S2SAuthRateLimit)
	apiGroup := e.Group("/api/v1")
	apihttp.RegisterConfigRoutes(apiGroup.Group("/config"), apihttp.NewConfigHandler(svc.Config, svc.Config, svc.Auth), apihttp.NewFeatureFlagHandler(svc.Flags, svc.Flags, svc.Auth), apiKeyAuthMW)
	apihttp.RegisterOIDCRoutes(e, apihttp.NewOIDCHandler(svc.OIDC))

	if notification.Enabled {
		notifAuthMW := apihttp.NewNotificationAPIKeyAuthMiddleware(
			auth.ServiceClientAuthenticator{Service: svc.Auth},
			svc.RateLimiter,
			cfg.AuthRateLimitWindowSeconds,
			cfg.S2SAuthRateLimit,
		)
		apihttp.RegisterNotificationRoutes(
			apiGroup.Group("/notifications"),
			apihttp.NewNotificationHandler(notif.Service, notif.Service, notif.Service, notif.Channels),
			apihttp.NewSessionHandler(notif.TokenIssuer, notif.Apps),
			apihttp.NewSSEHandler(notif.TokenIssuer, notif.Hub),
			apihttp.NewInAppHandler(notif.TokenIssuer, notif.Service),
			notifAuthMW,
		)
	}

	if err := dbos.Launch(dbosCtx); err != nil {
		log.Fatalf("launch dbos context: %v", err)
	}

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
	if err := dbos.Shutdown(dbosCtx, 10*time.Second); err != nil {
		slog.Error("dbos shutdown", "err", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown", "err", err)
	}
}
