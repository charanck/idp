package web

import (
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
)

// SetFullAccessPermissionsForTest attaches an unrestricted
// auth.EffectivePermissions (every module, every Application) to c. Exported
// for the web_test package's handler unit tests, which invoke handlers
// directly - bypassing router.go's ModuleRequired/AuthMiddleware.LoadUser
// chain that would normally populate this - and exercise CRUD business
// logic rather than authorization scoping (covered separately by the e2e
// tests).
func SetFullAccessPermissionsForTest(c echo.Context) {
	modules := make(map[string]bool, len(auth.AllModules))
	for _, m := range auth.AllModules {
		modules[m] = true
	}
	c.Set(contextKeyPermissions, auth.EffectivePermissions{
		Modules:          modules,
		UnrestrictedApps: true,
	})
}
