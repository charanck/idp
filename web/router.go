package web

import (
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	"controlplane/internal/notification"
)

// RegisterRoutes mounts every web UI page under "/", mirroring web_ui/urls.py.
// Session loading/CSRF protection must already be wired onto e (or a parent
// group) via session.Store.Middleware() and CSRFProtect().
func RegisterRoutes(e *echo.Echo, h *Handlers, authMW *AuthMiddleware) {
	e.Use(authMW.LoadUser())

	e.GET("/", h.Auth.Home)
	e.GET("/login/", h.Auth.Login)
	e.POST("/login/", h.Auth.Login)
	e.GET("/register/", h.Auth.Register)
	e.POST("/register/", h.Auth.Register)
	e.POST("/logout/", h.Auth.Logout)

	authed := e.Group("", authMW.LoginRequired())
	authed.GET("/password/change/", h.Auth.PasswordChange)
	authed.POST("/password/change/", h.Auth.PasswordChange)
	authed.GET("/dashboard/", h.Dashboard.Show)

	if notification.Enabled {
		authed.GET("/notifications/", h.Notification.List)
		authed.GET("/notifications/:id/", h.Notification.Detail)
	}

	configs := e.Group("", authMW.ModuleRequired(auth.ModuleConfigs))
	configs.GET("/configs/", h.Config.List)
	configs.GET("/configs/create/", h.Config.Create)
	configs.POST("/configs/create/", h.Config.Create)
	configs.GET("/configs/:id/clone/", h.Config.Clone)
	configs.POST("/configs/:id/clone/", h.Config.Clone)
	configs.GET("/configs/:id/edit/", h.Config.Edit)
	configs.POST("/configs/:id/edit/", h.Config.Edit)
	configs.GET("/configs/:id/delete/", h.Config.Delete)
	configs.POST("/configs/:id/delete/", h.Config.Delete)
	configs.GET("/configs/:id/history/", h.Config.History)
	configs.POST("/configs/:id/rollback/:version/", h.Config.Rollback)

	flags := e.Group("", authMW.ModuleRequired(auth.ModuleFlags))
	flags.GET("/flags/", h.Flag.List)
	flags.GET("/flags/create/", h.Flag.Create)
	flags.POST("/flags/create/", h.Flag.Create)
	flags.POST("/flags/:id/toggle/", h.Flag.Toggle)
	flags.POST("/flags/:id/delete/", h.Flag.Delete)
	flags.GET("/flags/:id/delete/", h.Flag.Delete)

	applications := e.Group("", authMW.ModuleRequired(auth.ModuleApplications))
	applications.GET("/applications/", h.Application.List)
	applications.POST("/applications/create/", h.Application.Create)
	applications.GET("/applications/create/", h.Application.Create)
	applications.GET("/applications/:id/edit/", h.Application.Edit)
	applications.POST("/applications/:id/edit/", h.Application.Edit)
	applications.GET("/applications/:id/delete/", h.Application.Delete)
	applications.POST("/applications/:id/delete/", h.Application.Delete)

	environments := e.Group("", authMW.ModuleRequired(auth.ModuleEnvironments))
	environments.GET("/environments/", h.Environment.List)
	environments.GET("/environments/create/", h.Environment.Create)
	environments.POST("/environments/create/", h.Environment.Create)
	environments.GET("/environments/:id/edit/", h.Environment.Edit)
	environments.POST("/environments/:id/edit/", h.Environment.Edit)
	environments.GET("/environments/:id/delete/", h.Environment.Delete)
	environments.POST("/environments/:id/delete/", h.Environment.Delete)

	users := e.Group("", authMW.ModuleRequired(auth.ModuleUsers))
	users.GET("/users/", h.User.List)
	users.GET("/users/create/", h.User.Create)
	users.POST("/users/create/", h.User.Create)
	users.GET("/users/:id/edit/", h.User.Edit)
	users.POST("/users/:id/edit/", h.User.Edit)
	users.GET("/users/:id/delete/", h.User.Delete)
	users.POST("/users/:id/delete/", h.User.Delete)

	groups := e.Group("", authMW.ModuleRequired(auth.ModuleGroups))
	groups.GET("/groups/", h.Group.List)
	groups.GET("/groups/create/", h.Group.Create)
	groups.POST("/groups/create/", h.Group.Create)
	groups.GET("/groups/:id/edit/", h.Group.Edit)
	groups.POST("/groups/:id/edit/", h.Group.Edit)
	groups.GET("/groups/:id/delete/", h.Group.Delete)
	groups.POST("/groups/:id/delete/", h.Group.Delete)

	clients := e.Group("", authMW.ModuleRequired(auth.ModuleServiceClients))
	clients.GET("/clients/", h.Client.List)
	clients.GET("/clients/create/", h.Client.Create)
	clients.POST("/clients/create/", h.Client.Create)
	clients.GET("/clients/:id/", h.Client.Detail)
	clients.GET("/clients/:id/edit/", h.Client.Edit)
	clients.POST("/clients/:id/edit/", h.Client.Edit)
	clients.POST("/clients/:id/toggle/", h.Client.Toggle)
	clients.GET("/clients/:id/delete/", h.Client.Delete)
	clients.POST("/clients/:id/delete/", h.Client.Delete)
	clients.POST("/clients/:id/regenerate-key/", h.Client.RegenerateKey)
	clients.GET("/clients/:id/regenerate-key/", h.Client.RegenerateKey)

	oauthProviders := e.Group("", authMW.ModuleRequired(auth.ModuleOAuthProviders))
	oauthProviders.GET("/oauth/providers/", h.OAuthProvider.List)
	oauthProviders.GET("/oauth/providers/create/", h.OAuthProvider.Create)
	oauthProviders.POST("/oauth/providers/create/", h.OAuthProvider.Create)
	oauthProviders.GET("/oauth/providers/:id/edit/", h.OAuthProvider.Edit)
	oauthProviders.POST("/oauth/providers/:id/edit/", h.OAuthProvider.Edit)
	oauthProviders.GET("/oauth/providers/:id/delete/", h.OAuthProvider.Delete)
	oauthProviders.POST("/oauth/providers/:id/delete/", h.OAuthProvider.Delete)
	oauthProviders.POST("/oauth/providers/:id/toggle/", h.OAuthProvider.Toggle)

	policies := e.Group("", authMW.ModuleRequired(auth.ModulePolicies))
	policies.GET("/policies/", h.Policy.Show)
	policies.POST("/policies/", h.Policy.Show)

	if notification.Enabled {
		notificationSettings := e.Group("", authMW.ModuleRequired(auth.ModuleNotificationSettings))
		notificationSettings.GET("/notification-settings/", h.NotificationSettings.List)
		notificationSettings.GET("/notification-settings/:channel/edit/", h.NotificationSettings.Edit)
		notificationSettings.POST("/notification-settings/:channel/edit/", h.NotificationSettings.Edit)
	}

	activityLog := e.Group("", authMW.ModuleRequired(auth.ModuleActivityLog))
	activityLog.GET("/activity/", h.Activity.List)

	e.GET("/oauth/login/:id/", h.OAuthLogin.Login)
	e.GET("/oauth/callback/:id/", h.OAuthLogin.Callback)

	e.GET("/oauth2/authorize", h.OIDC.Authorize)
	e.POST("/oauth2/authorize", h.OIDC.Authorize)
}
