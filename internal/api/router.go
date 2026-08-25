package api

import "github.com/labstack/echo/v4"

// RegisterConfigRoutes mounts the read-only S2S endpoints service clients
// poll for their own configs/secrets and feature flags, mirroring the
// listing routes in config_management/api.py. Everything else
// (create/update/delete/rollback of configs and flags) is web-UI-only now.
func RegisterConfigRoutes(g *echo.Group, configs *ConfigHandler, flags *FeatureFlagHandler, authMW *APIKeyAuthMiddleware) {
	g.GET("/configs/list", configs.List, authMW.Middleware())
	g.GET("/feature-flags", flags.List, authMW.Middleware())
}
