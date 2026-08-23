// Package api contains the Echo HTTP layer for the read-only S2S JSON API
// under /api/v1/config/..., mirroring the subset of config_management/api.py
// exposed to service clients (listing configs and feature flags). User-facing
// management (creating configs/secrets/flags/service clients) lives entirely
// in the web UI now - see internal/webui.
package api

import (
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
)

const contextKeyServiceClient = "service_client"

// ServiceClientFromContext returns the authenticated service client attached
// by APIKeyAuth, if any.
func ServiceClientFromContext(c echo.Context) *auth.ServiceClient {
	sc, _ := c.Get(contextKeyServiceClient).(*auth.ServiceClient)
	return sc
}
