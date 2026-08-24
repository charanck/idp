package notification

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// consumeUnreadInAppHandler lists the bearer token's authorized user's
// unread InApp notifications and marks them read. This is the only channel
// with a pull-based inbox - see ConsumeUnreadInAppForUser.
func consumeUnreadInAppHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
		if token == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
		}
		userID, err := deps.TokenIssuer.Validate(token)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
		}

		notifications, err := deps.Notifications.ConsumeUnreadInAppForUser(c.Request().Context(), userID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list unread notifications")
		}

		out := make([]notificationResponse, 0, len(notifications))
		for i := range notifications {
			out = append(out, toNotificationResponse(&notifications[i]))
		}
		return c.JSON(http.StatusOK, out)
	}
}
