// Package webui contains the server-rendered (session-authenticated) admin
// UI: Echo handlers + templ pages mirroring web_ui/views.py, forms.py,
// urls.py and templates/web_ui/*.html.
package webui

import (
	"controlplane/internal/activity"
	"controlplane/internal/auth"
	"controlplane/internal/config"
	"controlplane/internal/dashboard"
	"controlplane/internal/notification"
	"controlplane/internal/ratelimit"
	"controlplane/internal/session"
)

// Deps bundles the shared dependencies every web UI handler/middleware needs.
type Deps struct {
	AuthService          *auth.AuthService
	OAuthService         *auth.OAuthService
	ConfigService        *config.ConfigService
	FlagService          *config.FeatureFlagService
	Dashboard            *dashboard.Service
	Activity             *activity.Logger
	NotificationSettings *notification.ProviderSettingService
	RateLimiter          *ratelimit.Limiter
	Sessions             *session.Store

	AuthRateLimit              int
	AuthRateLimitWindowSeconds int
}
