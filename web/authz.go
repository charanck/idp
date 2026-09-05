package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
)

const contextKeyPermissions = "webui_effective_permissions"

// GroupPermissionLoader is what AuthMiddleware needs to compute a user's
// effective permissions from their group memberships. Satisfied by
// *auth.AuthService.
type GroupPermissionLoader interface {
	UserGroups(ctx context.Context, userID uuid.UUID) ([]authmodel.Group, error)
	GroupApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error)
}

// EffectivePermissionsFromContext returns the current user's computed
// permissions, attached by AuthMiddleware.LoadUser. Zero value (no modules,
// restricted to zero applications) if there is no logged-in user.
func EffectivePermissionsFromContext(c echo.Context) auth.EffectivePermissions {
	perms, _ := c.Get(contextKeyPermissions).(auth.EffectivePermissions)
	return perms
}

// AllowedApplicationIDs returns the current user's Application allow-list
// (nil + true if unrestricted) for handlers to scope List/Create/Edit
// operations by.
func AllowedApplicationIDs(c echo.Context) (ids []uuid.UUID, unrestricted bool) {
	return EffectivePermissionsFromContext(c).AllowedApplicationIDs()
}

// ApplicationAllowed reports whether the current user's Application
// allow-list permits appID (true if unrestricted or appID is in the list).
func ApplicationAllowed(c echo.Context, appID uuid.UUID) bool {
	perms := EffectivePermissionsFromContext(c)
	if perms.UnrestrictedApps {
		return true
	}
	return perms.AllowedAppIDs[appID]
}

// ModuleRequired is LoginRequired plus an effective-permissions check,
// flashing an error and redirecting to the dashboard when the logged-in
// user's groups don't grant module. Replaces the old is_staff-based
// AdminRequired.
func (m *AuthMiddleware) ModuleRequired(module string) echo.MiddlewareFunc {
	loginRequired := m.LoginRequired()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return loginRequired(func(c echo.Context) error {
			if !EffectivePermissionsFromContext(c).HasModule(module) {
				AddFlash(c, "danger", "You do not have permission to access this page.")
				return c.Redirect(http.StatusFound, "/dashboard/")
			}
			return next(c)
		})
	}
}
