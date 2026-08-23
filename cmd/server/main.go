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
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	otelecho "go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"gorm.io/gorm"
	gormtracing "gorm.io/plugin/opentelemetry/tracing"

	"controlplane/internal/activity"
	"controlplane/internal/api"
	"controlplane/internal/appconfig"
	"controlplane/internal/auth"
	"controlplane/internal/cache"
	"controlplane/internal/config"
	"controlplane/internal/crypto"
	"controlplane/internal/db"
	"controlplane/internal/observability"
	"controlplane/internal/ratelimit"
	"controlplane/internal/security"
	"controlplane/internal/session"
	"controlplane/internal/webui"
)

// version is the service.version resource attribute reported to OTEL,
// overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx := context.Background()

	otelShutdown, err := observability.Setup(ctx, version)
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

	gdb, err := db.Open(cfg.DSN(), cfg.Debug)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	if observability.Enabled() {
		if err := gdb.Use(gormtracing.NewPlugin(gormtracing.WithDBSystem("postgresql"))); err != nil {
			log.Fatalf("install gorm otel plugin: %v", err)
		}
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		log.Fatalf("get sql.DB: %v", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("parse REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(redisOpts)
	if observability.Enabled() {
		if err := redisotel.InstrumentTracing(rdb); err != nil {
			log.Fatalf("install redis otel tracing: %v", err)
		}
		if err := redisotel.InstrumentMetrics(rdb); err != nil {
			log.Fatalf("install redis otel metrics: %v", err)
		}
	}
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("connect to redis: %v", err)
	}

	var appCache cache.Cache = cache.NewRedisCache(rdb, cfg.CacheKeyPrefix)
	cacheTimeout := time.Duration(cfg.CacheTimeoutSeconds) * time.Second

	encryption := crypto.NewEncryptionService(cfg.MasterEncryptionKey)
	authService := auth.NewAuthService(gdb)
	oauthService := auth.NewOAuthService(gdb)
	configService := config.NewConfigService(gdb, encryption, appCache, cacheTimeout)
	flagService := config.NewFeatureFlagService(gdb, appCache, cacheTimeout)
	activityLogger := activity.NewLogger(gdb)
	limiter := ratelimit.NewLimiter(rdb)
	sessionStore := session.NewStore(rdb, cfg.SessionSecret, 0)

	if err := bootstrapAdmin(gdb, cfg); err != nil {
		log.Fatalf("bootstrap admin user: %v", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	if observability.Enabled() {
		e.Use(otelecho.Middleware(observability.ServiceName))
	}
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true, LogURI: true, LogMethod: true, LogLatency: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			slog.Info("request", "method", v.Method, "uri", v.URI, "status", v.Status, "latency", v.Latency)
			return nil
		},
	}))

	e.Static("/static", "static")

	// The API is stateless (JWT/API-key auth); the web UI needs session
	// loading + CSRF protection. Both are mounted on the same *echo.Echo, so
	// skip session/CSRF entirely for API paths rather than have CSRF's
	// form-field check reject JSON API requests.
	e.Use(skipForAPI(sessionStore.Middleware()))
	e.Use(skipForAPI(webui.CSRFProtect()))

	webuiDeps := &webui.Deps{
		DB:            gdb,
		AuthService:   authService,
		OAuthService:  oauthService,
		ConfigService: configService,
		FlagService:   flagService,
		Activity:      activityLogger,
		RateLimiter:   limiter,
		Sessions:      sessionStore,
		Encryption:    encryption,

		AuthRateLimit:              cfg.AuthRateLimit,
		AuthRateLimitWindowSeconds: cfg.AuthRateLimitWindowSeconds,
	}
	webui.RegisterRoutes(e, webuiDeps)

	apiDeps := &api.Deps{
		AuthService:   authService,
		ConfigService: configService,
		FlagService:   flagService,
		RateLimiter:   limiter,

		AuthRateLimitWindowSeconds: cfg.AuthRateLimitWindowSeconds,
		S2SAuthRateLimit:           cfg.S2SAuthRateLimit,
	}
	apiGroup := e.Group("/api/v1")
	api.RegisterConfigRoutes(apiGroup.Group("/config"), apiDeps)

	slog.Info("starting server", "port", cfg.Port)
	if err := e.Start(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

// skipForAPI wraps a middleware so it's a no-op for /api/... requests,
// letting the web UI's session/CSRF middleware share an *echo.Echo with the
// stateless JSON API.
func skipForAPI(mw echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		wrapped := mw(next)
		return func(c echo.Context) error {
			if strings.HasPrefix(c.Request().URL.Path, "/api/") {
				return next(c)
			}
			return wrapped(c)
		}
	}
}

// bootstrapAdmin mirrors authentication/management/commands/setup_admin.py:
// on every startup, ADMIN_EMAIL/ADMIN_PASSWORD (if set) create the admin user
// if missing, or sync an existing user's superuser/staff/active flags and
// password. A missing ADMIN_EMAIL is a no-op, not an error, matching the
// management command's behavior when run with no email available.
func bootstrapAdmin(gdb *gorm.DB, cfg *appconfig.Config) error {
	if cfg.AdminEmail == "" {
		return nil
	}
	password := cfg.AdminPassword
	if password == "" {
		password = "admin123"
	}

	var user auth.User
	err := gdb.Where("email = ?", cfg.AdminEmail).First(&user).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		hashed, hashErr := security.HashPassword(password)
		if hashErr != nil {
			return hashErr
		}
		newUser := auth.User{
			Email:              cfg.AdminEmail,
			Username:           strings.SplitN(cfg.AdminEmail, "@", 2)[0],
			Password:           hashed,
			IsSuperuser:        true,
			IsStaff:            true,
			IsActive:           true,
			ForcePasswordReset: true,
		}
		if createErr := gdb.Create(&newUser).Error; createErr != nil {
			return createErr
		}
		slog.Info("created admin user", "email", cfg.AdminEmail)
		return nil
	case err != nil:
		return err
	default:
		hashed, hashErr := security.HashPassword(password)
		if hashErr != nil {
			return hashErr
		}
		user.IsSuperuser = true
		user.IsStaff = true
		user.IsActive = true
		user.Password = hashed
		user.ForcePasswordReset = true
		if saveErr := gdb.Save(&user).Error; saveErr != nil {
			return saveErr
		}
		slog.Info("synced admin user credentials", "email", cfg.AdminEmail)
		return nil
	}
}
