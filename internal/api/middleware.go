package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"controlplane/internal/ratelimit"
)

const s2sRateLimitBucket = "s2s-api-key"

// APIKeyAuth authenticates the "X-API-Key: <key_id>.<secret>" S2S header,
// throttled by a plain fixed-window request counter per client IP (every
// request counts toward the window, not just failed ones).
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

			client, err := deps.AuthService.AuthenticateServiceAPIKey(c.Request().Context(), apiKey)
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
