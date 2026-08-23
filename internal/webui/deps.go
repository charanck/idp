// Package webui contains the server-rendered (session-authenticated) admin
// UI: Echo handlers + templ pages mirroring web_ui/views.py, forms.py,
// urls.py and templates/web_ui/*.html.
package webui

import (
	"gorm.io/gorm"

	"controlplane/internal/activity"
	"controlplane/internal/auth"
	"controlplane/internal/config"
	"controlplane/internal/crypto"
	"controlplane/internal/ratelimit"
	"controlplane/internal/session"
)

// Deps bundles the shared dependencies every web UI handler/middleware needs.
type Deps struct {
	DB            *gorm.DB // used directly only for cross-scope reads no service method covers (dashboard counts, admin CRUD by ID)
	AuthService   *auth.AuthService
	OAuthService  *auth.OAuthService
	ConfigService *config.ConfigService
	FlagService   *config.FeatureFlagService
	Activity      *activity.Logger
	RateLimiter   *ratelimit.Limiter
	Sessions      *session.Store

	// Encryption is a standalone encryption service (same master key as
	// ConfigService's, which keeps its own instance private) used only by
	// the config_edit handler, which - mirroring config_management's
	// web_ui/views.py config_edit - bypasses ConfigService.UpsertConfig to
	// mutate the ConfigEntry's fields directly before saving.
	Encryption *crypto.EncryptionService

	AuthRateLimit              int
	AuthRateLimitWindowSeconds int
}
