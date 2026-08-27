package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"controlplane/internal/ratelimit"
)

// notificationS2SRateLimitBucket is its own bucket name (distinct from
// s2sRateLimitBucket) so this module's auth failures don't share/exhaust the
// config API's rate-limit counter.
const notificationS2SRateLimitBucket = "s2s-notification-api-key"

// NotificationAuthenticator verifies an S2S API key and returns the calling
// client's identity (its Name, in auth.ServiceClient terms), or "" if the
// key is invalid/unknown. This is distinct from APIKeyAuthenticator because
// it deliberately does not import internal/auth: auth is expected to depend
// on notification later (to send password-reset/2FA emails), so notification
// importing auth back would be a cycle - main.go wires the concrete adapter
// instead, via NotificationAuthenticatorFunc.
type NotificationAuthenticator interface {
	Authenticate(ctx context.Context, apiKey string) (subject string, err error)
}

// NotificationAuthenticatorFunc adapts a plain function to
// NotificationAuthenticator, the same pattern as http.HandlerFunc - avoids a
// dedicated adapter type/file for what's just a one-method wrapper around
// auth.AuthService.
type NotificationAuthenticatorFunc func(ctx context.Context, apiKey string) (string, error)

func (f NotificationAuthenticatorFunc) Authenticate(ctx context.Context, apiKey string) (string, error) {
	return f(ctx, apiKey)
}

const contextKeyNotificationSubject = "notification_subject"

// SubjectFromContext returns the calling client's identity authenticated by
// NotificationAPIKeyAuthMiddleware for this request, or "" if none.
func SubjectFromContext(c echo.Context) string {
	subject, _ := c.Get(contextKeyNotificationSubject).(string)
	return subject
}

// NotificationAPIKeyAuthMiddleware authenticates the
// "X-API-Key: <key_id>.<secret>" S2S header via the configured
// NotificationAuthenticator (in production, auth.AuthService's
// service-client verification), throttled by a plain fixed-window request
// counter per client IP. This is its own copy of APIKeyAuthMiddleware rather
// than a shared extraction - the config API is scoped by its own doc
// comment, and this repo favors small self-contained packages over
// premature sharing.
type NotificationAPIKeyAuthMiddleware struct {
	authenticator NotificationAuthenticator
	limiter       RateLimiter
	windowSeconds int
	limit         int
}

func NewNotificationAPIKeyAuthMiddleware(authenticator NotificationAuthenticator, limiter RateLimiter, windowSeconds, limit int) *NotificationAPIKeyAuthMiddleware {
	return &NotificationAPIKeyAuthMiddleware{authenticator: authenticator, limiter: limiter, windowSeconds: windowSeconds, limit: limit}
}

func (m *NotificationAPIKeyAuthMiddleware) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			apiKey := c.Request().Header.Get("X-API-Key")
			if apiKey == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())
			window := time.Duration(m.windowSeconds) * time.Second
			limited, err := m.limiter.IsRateLimited(c.Request().Context(), notificationS2SRateLimitBucket, clientIP, m.limit, window)
			if err != nil {
				return err
			}
			if limited {
				c.Response().Header().Set("Retry-After", strconv.Itoa(m.windowSeconds))
				return c.String(http.StatusTooManyRequests, "Too many requests. Please try again later.")
			}

			subject, err := m.authenticator.Authenticate(c.Request().Context(), apiKey)
			if err != nil {
				return err
			}
			if subject == "" {
				slog.Warn("notification API key auth rejected", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			c.Set(contextKeyNotificationSubject, subject)
			return next(c)
		}
	}
}
