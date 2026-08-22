// Package api contains the Echo HTTP layer: JWT/API-key middleware and
// route handlers for /api/v1/..., mirroring common/authentication.py and
// each Django app's api.py.
package api

import (
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
)

const (
	contextKeyUser          = "user"
	contextKeyServiceClient = "service_client"
)

// UserFromContext returns the authenticated user attached by JWTAuth, if any.
func UserFromContext(c echo.Context) *auth.User {
	u, _ := c.Get(contextKeyUser).(*auth.User)
	return u
}

// ServiceClientFromContext returns the authenticated service client attached
// by APIKeyAuth, if any.
func ServiceClientFromContext(c echo.Context) *auth.ServiceClient {
	sc, _ := c.Get(contextKeyServiceClient).(*auth.ServiceClient)
	return sc
}
