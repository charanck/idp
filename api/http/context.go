// Package api contains the Echo HTTP layer for the read-only S2S JSON API
// under /api/v1/config/..., mirroring the subset of config_management/api.py
// exposed to service clients (listing configs and feature flags). User-facing
// management (creating configs/secrets/flags/service clients) lives entirely
// in the web UI now - see web.
package http

import (
	"github.com/labstack/echo/v4"

	authmodel "controlplane/internal/model/auth"
)

const contextKeyServiceClient = "service_client"

// ServiceClientFromContext returns the authenticated service client attached
// by APIKeyAuth, if any.
func ServiceClientFromContext(c echo.Context) *authmodel.ServiceClient {
	sc, _ := c.Get(contextKeyServiceClient).(*authmodel.ServiceClient)
	return sc
}
