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
		LogStatus: true, LogURI: true, LogMethod: true, LogLatency: true, LogRemoteIP: true, LogError: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			// Uses the request's own context (not context.Background()) so a
			// span started by otelecho.Middleware above is still attached,
			// letting OTEL correlate this log line with its trace/span ID.
			ctx := c.Request().Context()
			attrs := []any{"method", v.Method, "uri", v.URI, "status", v.Status, "latency", v.Latency, "remote_ip", v.RemoteIP}
			if v.Error != nil {
				attrs = append(attrs, "err", v.Error)
			}
			switch {
			case v.Status >= 500 || v.Error != nil:
				slog.ErrorContext(ctx, "request", attrs...)
			case v.Status >= 400:
				slog.WarnContext(ctx, "request", attrs...)
			default:
				slog.InfoContext(ctx, "request", attrs...)
			}
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

// skipForStatelessAPI wraps a middleware so it's a no-op for /api/... paths,
// the stateless OIDC endpoints, and public static assets (/static/...,
// /favicon.ico), letting the web UI's session/CSRF middleware share an
// *echo.Echo with the stateless JSON API. Static assets must be excluded:
// e.Static registers a plain route still wrapped by e.Use middleware, so
// without this, concurrent asset requests on a cookie-less page load would
// each mint and persist their own session/CSRF token, racing the page's own
// cookie and causing CSRF validation to fail unpredictably.
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
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/.well-known/") || strings.HasPrefix(path, "/static/") {
		return true
	}
	return path == "/oauth2/token" || path == "/oauth2/userinfo" || path == "/favicon.ico"
}
