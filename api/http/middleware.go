package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	"controlplane/internal/ratelimit"
)

const s2sRateLimitBucket = "s2s-api-key"

// APIKeyAuthenticator is the narrow slice of *auth.AuthService that
// APIKeyAuthMiddleware needs.
type APIKeyAuthenticator interface {
	AuthenticateServiceAPIKey(ctx context.Context, apiKey string) (*auth.ServiceClient, error)
}

// RateLimiter is the narrow slice of *ratelimit.Limiter that
// APIKeyAuthMiddleware needs.
type RateLimiter interface {
	IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error)
}

// APIKeyAuthMiddleware authenticates the "X-API-Key: <key_id>.<secret>" S2S
// header, throttled by a plain fixed-window request counter per client IP
// (every request counts toward the window, not just failed ones).
type APIKeyAuthMiddleware struct {
	authenticator APIKeyAuthenticator
	limiter       RateLimiter
	windowSeconds int
	limit         int
}

func NewAPIKeyAuthMiddleware(authenticator APIKeyAuthenticator, limiter RateLimiter, windowSeconds, limit int) *APIKeyAuthMiddleware {
	return &APIKeyAuthMiddleware{authenticator: authenticator, limiter: limiter, windowSeconds: windowSeconds, limit: limit}
}

// Middleware returns the echo.MiddlewareFunc enforcing S2S API-key auth.
func (m *APIKeyAuthMiddleware) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			apiKey := c.Request().Header.Get("X-API-Key")
			if apiKey == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())
			window := time.Duration(m.windowSeconds) * time.Second
			limited, err := m.limiter.IsRateLimited(c.Request().Context(), s2sRateLimitBucket, clientIP, m.limit, window)
			if err != nil {
				return err
			}
			if limited {
				c.Response().Header().Set("Retry-After", strconv.Itoa(m.windowSeconds))
				return c.String(http.StatusTooManyRequests, "Too many requests. Please try again later.")
			}

			client, err := m.authenticator.AuthenticateServiceAPIKey(c.Request().Context(), apiKey)
			if err != nil {
				return err
			}
			if client == nil {
				keyID := "<malformed>"
				if id, _, ok := strings.Cut(apiKey, "."); ok {
					keyID = id
				}
				slog.Warn("API key auth rejected", "key_id", keyID, "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			c.Set(contextKeyServiceClient, client)
			return next(c)
		}
	}
}

var _ APIKeyAuthenticator = (*auth.AuthService)(nil)
var _ RateLimiter = (*ratelimit.Limiter)(nil)
