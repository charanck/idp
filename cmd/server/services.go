package main

import (
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/activity"
	"controlplane/internal/appconfig"
	"controlplane/internal/auth"
	"controlplane/internal/cache"
	"controlplane/internal/config"
	"controlplane/internal/crypto"
	"controlplane/internal/dashboard"
	"controlplane/internal/ratelimit"
	"controlplane/internal/session"
)

// coreServices bundles every domain service built directly from
// infrastructure (db/redis), so main() has one value to pass around instead
// of a dozen loose locals.
type coreServices struct {
	Encryption  *crypto.EncryptionService
	Auth        *auth.AuthService
	OAuth       *auth.OAuthService
	Config      *config.ConfigService
	Flags       *config.FeatureFlagService
	Activity    *activity.Logger
	Dashboard   *dashboard.Service
	Cache       cache.Cache
	RateLimiter *ratelimit.Limiter
	Sessions    *session.Store
}

func newCoreServices(gdb *gorm.DB, rdb *redis.Client, cfg *appconfig.Config) *coreServices {
	var appCache cache.Cache = cache.NewRedisCache(rdb, cfg.CacheKeyPrefix)
	cacheTimeout := time.Duration(cfg.CacheTimeoutSeconds) * time.Second

	encryption := crypto.NewEncryptionService(cfg.MasterEncryptionKey)

	return &coreServices{
		Encryption:  encryption,
		Auth:        auth.NewAuthService(auth.NewUserRepository(gdb), auth.NewServiceClientRepository(gdb)),
		OAuth:       auth.NewOAuthService(auth.NewOAuthProviderRepository(gdb), auth.NewOAuthUserTokenRepository(gdb), auth.NewUserRepository(gdb)),
		Config:      config.NewConfigService(config.NewConfigRepository(gdb), config.NewApplicationRepository(gdb), config.NewEnvironmentRepository(gdb), encryption, appCache, cacheTimeout),
		Flags:       config.NewFeatureFlagService(config.NewFeatureFlagRepository(gdb), config.NewApplicationRepository(gdb), config.NewEnvironmentRepository(gdb), appCache, cacheTimeout),
		Activity:    activity.NewLogger(activity.NewRepository(gdb)),
		Dashboard:   dashboard.NewService(dashboard.NewRepository(gdb)),
		Cache:       appCache,
		RateLimiter: ratelimit.NewLimiter(rdb),
		Sessions:    session.NewStore(rdb, cfg.SessionSecret, 0),
	}
}
