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
	// skip session/CSRF entirely for API paths (and the equally stateless
	// OIDC discovery/token/userinfo endpoints) rather than have CSRF's
	// form-field check reject JSON API requests.
	e.Use(skipForStatelessAPI(sessions.Middleware()))
	e.Use(skipForStatelessAPI(web.CSRFProtect()))

	return e
}

// skipForStatelessAPI wraps a middleware so it's a no-op for /api/... paths
// and the stateless OIDC endpoints, letting the web UI's session/CSRF
// middleware share an *echo.Echo with the stateless JSON API.
// /oauth2/authorize is deliberately excluded - it's browser-facing and
// still needs session auth + CSRF.
func skipForStatelessAPI(mw echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		wrapped := mw(next)
		return func(c echo.Context) error {
			if isStatelessAPIPath(c.Request().URL.Path) {
				return next(c)
			}
			return wrapped(c)
		}
	}
}

func isStatelessAPIPath(path string) bool {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/.well-known/") {
		return true
	}
	return path == "/oauth2/token" || path == "/oauth2/userinfo"
}
