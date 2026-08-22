package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/jwtutil"
	"controlplane/internal/ratelimit"
)

const s2sRateLimitBucket = "s2s-api-key"

func bearerToken(c echo.Context) (string, bool) {
	h := c.Request().Header.Get("Authorization")
	token, ok := strings.CutPrefix(h, "Bearer ")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(token), token != ""
}

// JWTAuth requires a valid "user"-type Bearer JWT for an active user,
// mirroring common/authentication.py's JWTAuth.
func JWTAuth(deps *Deps) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token, ok := bearerToken(c)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			claims, err := jwtutil.DecodeAccessToken(deps.JWTSecretKey, token)
			if err != nil {
				slog.Warn("JWT auth rejected", "path", c.Path(), "err", err)
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			if claims["type"] != "user" {
				slog.Warn("JWT auth rejected: wrong token type", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			sub, _ := claims["sub"].(string)
			userID, err := uuid.Parse(sub)
			if err != nil {
				slog.Warn("JWT auth rejected: missing subject claim", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			user, err := deps.AuthService.GetUserByID(userID)
			if err != nil {
				return err
			}
			if user == nil {
				slog.Warn("JWT auth rejected: user not found or inactive", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			c.Set(contextKeyUser, user)
			return next(c)
		}
	}
}

// JWTAdminAuth is JWTAuth restricted to staff users, mirroring
// common/authentication.py's JWTAdminAuth.
func JWTAdminAuth(deps *Deps) echo.MiddlewareFunc {
	inner := JWTAuth(deps)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return inner(func(c echo.Context) error {
			user := UserFromContext(c)
			if user == nil || !user.IsStaff {
				slog.Warn("JWT admin auth rejected: user is not staff", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}
			return next(c)
		})
	}
}

// APIKeyAuth authenticates the "X-API-Key: <key_id>.<secret>" S2S header,
// with a failure-rate-limit bucket per client IP, mirroring
// common/authentication.py's APIKeyAuth.
func APIKeyAuth(deps *Deps) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			apiKey := c.Request().Header.Get("X-API-Key")
			if apiKey == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())
			ctx := c.Request().Context()

			limited, err := deps.RateLimiter.IsFailureLimited(ctx, s2sRateLimitBucket, clientIP, deps.S2SAuthRateLimit)
			if err != nil {
				return err
			}
			if limited {
				slog.Warn("API key auth blocked: too many failed attempts from this client", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			client, err := deps.AuthService.AuthenticateServiceAPIKey(apiKey)
			if err != nil {
				return err
			}
			if client == nil {
				keyID := "<malformed>"
				if id, _, ok := strings.Cut(apiKey, "."); ok {
					keyID = id
				}
				slog.Warn("API key auth rejected", "key_id", keyID, "path", c.Path())
				window := time.Duration(deps.AuthRateLimitWindowSeconds) * time.Second
				if recErr := deps.RateLimiter.RecordFailure(ctx, s2sRateLimitBucket, clientIP, window); recErr != nil {
					return recErr
				}
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			c.Set(contextKeyServiceClient, client)
			return next(c)
		}
	}
}

// RateLimit throttles POST requests to a fixed auth endpoint per client IP,
// mirroring common/middleware.py's RateLimitMiddleware LIMITED_PATHS entries
// for "auth-token"/"auth-register".
func RateLimit(deps *Deps, bucket string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())
			window := time.Duration(deps.AuthRateLimitWindowSeconds) * time.Second
			limited, err := deps.RateLimiter.IsRateLimited(c.Request().Context(), bucket, clientIP, deps.AuthRateLimit, window)
			if err != nil {
				return err
			}
			if limited {
				c.Response().Header().Set("Retry-After", strconv.Itoa(deps.AuthRateLimitWindowSeconds))
				return c.String(http.StatusTooManyRequests, "Too many requests. Please try again later.")
			}
			return next(c)
		}
	}
}

// JWTOrAPIKeyAuth accepts either a user Bearer JWT or an S2S X-API-Key
// header, mirroring routes declared with auth=[jwt_auth, apikey_auth] in
// common/authentication.py-based Ninja routers.
func JWTOrAPIKeyAuth(deps *Deps) echo.MiddlewareFunc {
	jwtAuth := JWTAuth(deps)
	apiKeyAuth := APIKeyAuth(deps)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		jwtNext := jwtAuth(next)
		apiKeyNext := apiKeyAuth(next)
		return func(c echo.Context) error {
			if _, ok := bearerToken(c); ok {
				return jwtNext(c)
			}
			return apiKeyNext(c)
		}
	}
}
