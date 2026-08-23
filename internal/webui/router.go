package webui

import (
	"github.com/labstack/echo/v4"
)

// RegisterRoutes mounts every web UI page under "/", mirroring web_ui/urls.py.
// Session loading/CSRF protection must already be wired onto e (or a parent
// group) via deps.Sessions.Middleware() and CSRFProtect().
func RegisterRoutes(e *echo.Echo, deps *Deps) {
	e.Use(LoadUser(deps))

	e.GET("/", homeHandler(deps))
	e.GET("/login/", loginViewHandler(deps))
	e.POST("/login/", loginViewHandler(deps))
	e.GET("/register/", registerViewHandler(deps))
	e.POST("/register/", registerViewHandler(deps))
	e.POST("/logout/", logoutViewHandler(deps))

	authed := e.Group("", LoginRequired(deps))
	authed.GET("/password/change/", passwordChangeViewHandler(deps))
	authed.POST("/password/change/", passwordChangeViewHandler(deps))
	authed.GET("/dashboard/", dashboardHandler(deps))

	authed.GET("/configs/", configsListHandler(deps))
	authed.GET("/configs/create/", configCreateHandler(deps))
	authed.POST("/configs/create/", configCreateHandler(deps))
	authed.GET("/configs/:id/clone/", configCloneHandler(deps))
	authed.POST("/configs/:id/clone/", configCloneHandler(deps))
	authed.GET("/configs/:id/edit/", configEditHandler(deps))
	authed.POST("/configs/:id/edit/", configEditHandler(deps))
	authed.GET("/configs/:id/delete/", configDeleteHandler(deps))
	authed.POST("/configs/:id/delete/", configDeleteHandler(deps))
	authed.GET("/configs/:id/history/", configHistoryHandler(deps))
	authed.POST("/configs/:id/rollback/:version/", configRollbackHandler(deps))

	authed.GET("/flags/", flagsListHandler(deps))
	authed.GET("/flags/create/", flagCreateHandler(deps))
	authed.POST("/flags/create/", flagCreateHandler(deps))
	authed.POST("/flags/:id/toggle/", flagToggleHandler(deps))
	authed.POST("/flags/:id/delete/", flagDeleteHandler(deps))
	authed.GET("/flags/:id/delete/", flagDeleteHandler(deps))

	admin := e.Group("", AdminRequired(deps))
	admin.GET("/applications/", applicationsListHandler(deps))
	admin.POST("/applications/create/", applicationCreateHandler(deps))
	admin.GET("/applications/create/", applicationCreateHandler(deps))
	admin.GET("/applications/:id/edit/", applicationEditHandler(deps))
	admin.POST("/applications/:id/edit/", applicationEditHandler(deps))
	admin.GET("/applications/:id/delete/", applicationDeleteHandler(deps))
	admin.POST("/applications/:id/delete/", applicationDeleteHandler(deps))

	admin.GET("/environments/", environmentsListHandler(deps))
	admin.GET("/environments/create/", environmentCreateHandler(deps))
	admin.POST("/environments/create/", environmentCreateHandler(deps))
	admin.GET("/environments/:id/edit/", environmentEditHandler(deps))
	admin.POST("/environments/:id/edit/", environmentEditHandler(deps))
	admin.GET("/environments/:id/delete/", environmentDeleteHandler(deps))
	admin.POST("/environments/:id/delete/", environmentDeleteHandler(deps))

	admin.GET("/users/", usersListHandler(deps))
	admin.GET("/users/create/", userCreateHandler(deps))
	admin.POST("/users/create/", userCreateHandler(deps))
	admin.GET("/users/:id/edit/", userEditHandler(deps))
	admin.POST("/users/:id/edit/", userEditHandler(deps))
	admin.GET("/users/:id/delete/", userDeleteHandler(deps))
	admin.POST("/users/:id/delete/", userDeleteHandler(deps))

	admin.GET("/clients/", clientsListHandler(deps))
	admin.GET("/clients/create/", clientCreateHandler(deps))
	admin.POST("/clients/create/", clientCreateHandler(deps))
	admin.GET("/clients/:id/", clientDetailHandler(deps))
	admin.POST("/clients/:id/toggle/", clientToggleHandler(deps))
	admin.GET("/clients/:id/delete/", clientDeleteHandler(deps))
	admin.POST("/clients/:id/delete/", clientDeleteHandler(deps))
	admin.POST("/clients/:id/regenerate-key/", clientRegenerateKeyHandler(deps))
	admin.GET("/clients/:id/regenerate-key/", clientRegenerateKeyHandler(deps))

	admin.GET("/oauth/providers/", oauthProvidersListHandler(deps))
	admin.GET("/oauth/providers/create/", oauthProviderCreateHandler(deps))
	admin.POST("/oauth/providers/create/", oauthProviderCreateHandler(deps))
	admin.GET("/oauth/providers/:id/edit/", oauthProviderEditHandler(deps))
	admin.POST("/oauth/providers/:id/edit/", oauthProviderEditHandler(deps))
	admin.GET("/oauth/providers/:id/delete/", oauthProviderDeleteHandler(deps))
	admin.POST("/oauth/providers/:id/delete/", oauthProviderDeleteHandler(deps))
	admin.POST("/oauth/providers/:id/toggle/", oauthProviderToggleHandler(deps))

	admin.GET("/activity/", activityLogHandler(deps))

	e.GET("/oauth/login/:id/", oauthLoginHandler(deps))
	e.GET("/oauth/callback/:id/", oauthCallbackHandler(deps))
}
