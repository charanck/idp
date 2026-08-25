package notification

import "github.com/labstack/echo/v4"

// RegisterRoutes mounts the notification S2S API under g (expected to be
// apiGroup.Group("/notifications")).
func RegisterRoutes(g *echo.Group, notifications *NotificationHandler, sessions *SessionHandler, sse *SSEHandler, inapp *InAppHandler, authMW *APIKeyAuthMiddleware) {
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
