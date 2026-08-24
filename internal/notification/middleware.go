package notification

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"controlplane/internal/ratelimit"
)

// s2sRateLimitBucket is its own bucket name (distinct from internal/api's
// "s2s-api-key") so this module's auth failures don't share/exhaust the
// config API's rate-limit counter.
const s2sRateLimitBucket = "s2s-notification-api-key"

// Authenticator verifies an S2S API key and returns the calling client's
// identity (its Name, in auth.ServiceClient terms), or "" if the key is
// invalid/unknown. This package deliberately does not import internal/auth:
// auth is expected to depend on notification later (to send password-reset/
// 2FA emails), so notification importing auth back would be a cycle -
// main.go wires the concrete adapter instead, via AuthenticatorFunc.
type Authenticator interface {
	Authenticate(ctx context.Context, apiKey string) (subject string, err error)
}

// AuthenticatorFunc adapts a plain function to Authenticator, the same
// pattern as http.HandlerFunc - avoids a dedicated adapter type/file for
// what's just a one-method wrapper around auth.AuthService.
type AuthenticatorFunc func(ctx context.Context, apiKey string) (string, error)

func (f AuthenticatorFunc) Authenticate(ctx context.Context, apiKey string) (string, error) {
	return f(ctx, apiKey)
}

const contextKeySubject = "notification_subject"

// SubjectFromContext returns the calling client's identity authenticated by
// APIKeyAuth for this request, or "" if none.
func SubjectFromContext(c echo.Context) string {
	subject, _ := c.Get(contextKeySubject).(string)
	return subject
}

// APIKeyAuth authenticates the "X-API-Key: <key_id>.<secret>" S2S header via
// the configured Authenticator (in production, auth.AuthService's
// service-client verification), throttled by a plain fixed-window request
// counter per client IP. This is its own copy of internal/api's APIKeyAuth
// rather than a shared extraction - internal/api is scoped to the config API
// by its own doc comment, and this repo favors small self-contained packages
// over premature sharing.
func APIKeyAuth(deps *Deps) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			apiKey := c.Request().Header.Get("X-API-Key")
			if apiKey == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())
			window := time.Duration(deps.AuthRateLimitWindowSeconds) * time.Second
			limited, err := deps.RateLimiter.IsRateLimited(c.Request().Context(), s2sRateLimitBucket, clientIP, deps.S2SAuthRateLimit, window)
			if err != nil {
				return err
			}
			if limited {
				c.Response().Header().Set("Retry-After", strconv.Itoa(deps.AuthRateLimitWindowSeconds))
				return c.String(http.StatusTooManyRequests, "Too many requests. Please try again later.")
			}

			subject, err := deps.Authenticator.Authenticate(c.Request().Context(), apiKey)
			if err != nil {
				return err
			}
			if subject == "" {
				slog.Warn("notification API key auth rejected", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			c.Set(contextKeySubject, subject)
			return next(c)
		}
	}
}
