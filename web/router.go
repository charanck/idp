package web

import (
	"github.com/labstack/echo/v4"

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

	authed.GET("/configs/", h.Config.List)
	authed.GET("/configs/create/", h.Config.Create)
	authed.POST("/configs/create/", h.Config.Create)
	authed.GET("/configs/:id/clone/", h.Config.Clone)
	authed.POST("/configs/:id/clone/", h.Config.Clone)
	authed.GET("/configs/:id/edit/", h.Config.Edit)
	authed.POST("/configs/:id/edit/", h.Config.Edit)
	authed.GET("/configs/:id/delete/", h.Config.Delete)
	authed.POST("/configs/:id/delete/", h.Config.Delete)
	authed.GET("/configs/:id/history/", h.Config.History)
	authed.POST("/configs/:id/rollback/:version/", h.Config.Rollback)

	authed.GET("/flags/", h.Flag.List)
	authed.GET("/flags/create/", h.Flag.Create)
	authed.POST("/flags/create/", h.Flag.Create)
	authed.POST("/flags/:id/toggle/", h.Flag.Toggle)
	authed.POST("/flags/:id/delete/", h.Flag.Delete)
	authed.GET("/flags/:id/delete/", h.Flag.Delete)

	if notification.Enabled {
		authed.GET("/notifications/", h.Notification.List)
	}

	admin := e.Group("", authMW.AdminRequired())
	admin.GET("/applications/", h.Application.List)
	admin.POST("/applications/create/", h.Application.Create)
	admin.GET("/applications/create/", h.Application.Create)
	admin.GET("/applications/:id/edit/", h.Application.Edit)
	admin.POST("/applications/:id/edit/", h.Application.Edit)
	admin.GET("/applications/:id/delete/", h.Application.Delete)
	admin.POST("/applications/:id/delete/", h.Application.Delete)

	admin.GET("/environments/", h.Environment.List)
	admin.GET("/environments/create/", h.Environment.Create)
	admin.POST("/environments/create/", h.Environment.Create)
	admin.GET("/environments/:id/edit/", h.Environment.Edit)
	admin.POST("/environments/:id/edit/", h.Environment.Edit)
	admin.GET("/environments/:id/delete/", h.Environment.Delete)
	admin.POST("/environments/:id/delete/", h.Environment.Delete)

	admin.GET("/users/", h.User.List)
	admin.GET("/users/create/", h.User.Create)
	admin.POST("/users/create/", h.User.Create)
	admin.GET("/users/:id/edit/", h.User.Edit)
	admin.POST("/users/:id/edit/", h.User.Edit)
	admin.GET("/users/:id/delete/", h.User.Delete)
	admin.POST("/users/:id/delete/", h.User.Delete)

	admin.GET("/clients/", h.Client.List)
	admin.GET("/clients/create/", h.Client.Create)
	admin.POST("/clients/create/", h.Client.Create)
	admin.GET("/clients/:id/", h.Client.Detail)
	admin.POST("/clients/:id/toggle/", h.Client.Toggle)
	admin.GET("/clients/:id/delete/", h.Client.Delete)
	admin.POST("/clients/:id/delete/", h.Client.Delete)
	admin.POST("/clients/:id/regenerate-key/", h.Client.RegenerateKey)
	admin.GET("/clients/:id/regenerate-key/", h.Client.RegenerateKey)

	admin.GET("/oauth/providers/", h.OAuthProvider.List)
	admin.GET("/oauth/providers/create/", h.OAuthProvider.Create)
	admin.POST("/oauth/providers/create/", h.OAuthProvider.Create)
	admin.GET("/oauth/providers/:id/edit/", h.OAuthProvider.Edit)
	admin.POST("/oauth/providers/:id/edit/", h.OAuthProvider.Edit)
	admin.GET("/oauth/providers/:id/delete/", h.OAuthProvider.Delete)
	admin.POST("/oauth/providers/:id/delete/", h.OAuthProvider.Delete)
	admin.POST("/oauth/providers/:id/toggle/", h.OAuthProvider.Toggle)

	if notification.Enabled {
		admin.GET("/notification-settings/", h.NotificationSettings.List)
		admin.GET("/notification-settings/:channel/edit/", h.NotificationSettings.Edit)
		admin.POST("/notification-settings/:channel/edit/", h.NotificationSettings.Edit)
	}

	admin.GET("/activity/", h.Activity.List)

	e.GET("/oauth/login/:id/", h.OAuthLogin.Login)
	e.GET("/oauth/callback/:id/", h.OAuthLogin.Callback)
}
