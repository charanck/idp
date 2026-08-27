package main

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	otelecho "go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

	"controlplane/internal/observability"
	"controlplane/internal/session"
	"controlplane/web"
)

// newEchoServer builds the shared *echo.Echo that serves both the stateless
// JSON API and the session-authenticated web UI, with logging/recover/static
// middleware and session+CSRF middleware skipped for /api/... paths.
func newEchoServer(sessions *session.Store) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	if observability.Enabled() {
		e.Use(otelecho.Middleware(observability.ServiceName))
	}
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true, LogURI: true, LogMethod: true, LogLatency: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			slog.Info("request", "method", v.Method, "uri", v.URI, "status", v.Status, "latency", v.Latency)
			return nil
		},
	}))

	e.Static("/static", "web/static")

	// The API is stateless (JWT/API-key auth); the web UI needs session
	// loading + CSRF protection. Both are mounted on the same *echo.Echo, so
	// skip session/CSRF entirely for API paths rather than have CSRF's
	// form-field check reject JSON API requests.
	e.Use(skipForAPI(sessions.Middleware()))
	e.Use(skipForAPI(web.CSRFProtect()))

	return e
}

// skipForAPI wraps a middleware so it's a no-op for /api/... requests,
// letting the web UI's session/CSRF middleware share an *echo.Echo with the
// stateless JSON API.
func skipForAPI(mw echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		wrapped := mw(next)
		return func(c echo.Context) error {
			if strings.HasPrefix(c.Request().URL.Path, "/api/") {
				return next(c)
			}
			return wrapped(c)
		}
	}
}
