package api

import (
	"controlplane/internal/auth"
	"controlplane/internal/config"
	"controlplane/internal/ratelimit"
)

// Deps bundles the shared dependencies every handler/middleware in this
// package needs, avoiding a global service-locator pattern.
type Deps struct {
	AuthService   *auth.AuthService
	ConfigService *config.ConfigService
	FlagService   *config.FeatureFlagService
	RateLimiter   *ratelimit.Limiter

	AuthRateLimitWindowSeconds int
	S2SAuthRateLimit           int
}
