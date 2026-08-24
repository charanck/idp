package notification

import "github.com/labstack/echo/v4"

// RegisterRoutes mounts the notification S2S API under g (expected to be
// apiGroup.Group("/notifications")).
func RegisterRoutes(g *echo.Group, deps *Deps) {
	auth := APIKeyAuth(deps)

	g.POST("", createNotificationHandler(deps), auth)
	g.GET("", listNotificationsHandler(deps), auth)
	g.GET("/:id", getNotificationHandler(deps), auth)

	g.POST("/sessions", createSessionHandler(deps), auth)
	// GET /sse/events and GET /inapp/unread are deliberately not wrapped in
	// APIKeyAuth (the caller is the end user, not the service client, and
	// can't send an X-API-Key) - they authenticate via the Fernet token
	// minted above instead, sent as an Authorization header.
	g.GET("/sse/events", sseEventsHandler(deps))
	g.GET("/inapp/unread", consumeUnreadInAppHandler(deps))
}
