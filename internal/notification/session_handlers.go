package notification

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

type createSessionRequest struct {
	UserID string `json:"user_id"`
}

type createSessionResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in_seconds"`
}

// createSessionHandler mints a short-lived bearer token authorizing its
// caller to act as userID against the endpoints that don't accept an
// X-API-Key: streaming their SSE events (sseEventsHandler) and listing/
// consuming their InApp unread inbox (consumeUnreadInAppHandler).
func createSessionHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req createSessionRequest
		if err := c.Bind(&req); err != nil || req.UserID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "user_id is required")
		}

		token, err := deps.TokenIssuer.Issue(req.UserID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to mint notification session")
		}
		return c.JSON(http.StatusOK, createSessionResponse{
			Token:     token,
			ExpiresIn: int(sessionTokenTTL.Seconds()),
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header value, or "" if the header is missing or malformed.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}
