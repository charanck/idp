package http

import "github.com/labstack/echo/v4"

// RegisterConfigRoutes mounts the read-only S2S endpoints service clients
// poll for their own configs/secrets and feature flags, mirroring the
// listing routes in config_management/api.py. Everything else
// (create/update/delete/rollback of configs and flags) is web-UI-only now.
func RegisterConfigRoutes(g *echo.Group, configs *ConfigHandler, flags *FeatureFlagHandler, authMW *APIKeyAuthMiddleware) {
	g.GET("/configs/list", configs.List, authMW.Middleware())
	g.GET("/feature-flags", flags.List, authMW.Middleware())
}

// RegisterNotificationRoutes mounts the notification S2S API under g
// (expected to be apiGroup.Group("/notifications")).
func RegisterNotificationRoutes(g *echo.Group, notifications *NotificationHandler, sessions *SessionHandler, sse *SSEHandler, inapp *InAppHandler, authMW *NotificationAPIKeyAuthMiddleware) {
	auth := authMW.Middleware()

	g.POST("", notifications.Create, auth)
	g.GET("", notifications.List, auth)
	g.GET("/:id", notifications.Get, auth)

	g.POST("/sessions", sessions.Create, auth)
	// GET /sse/events and GET /inapp/unread are deliberately not wrapped in
	// authMW (the caller is the end user, not the service client, and can't
	// send an X-API-Key) - they authenticate via the Fernet token minted
	// above instead, sent as an Authorization header.
	g.GET("/sse/events", sse.Stream)
	g.GET("/inapp/unread", inapp.ConsumeUnread)
}
