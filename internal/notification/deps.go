package notification

import "controlplane/internal/ratelimit"

// Deps bundles the shared dependencies every handler/middleware in this
// package needs.
type Deps struct {
	Authenticator Authenticator
	Notifications *NotificationService
	Channels      ChannelRegistry
	RateLimiter   *ratelimit.Limiter
	Hub           *Hub
	TokenIssuer   *TokenIssuer

	AuthRateLimitWindowSeconds int
	S2SAuthRateLimit           int
}
