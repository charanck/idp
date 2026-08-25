package notification

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

// UnreadConsumer is the narrow slice of *NotificationService that
// InAppHandler needs.
type UnreadConsumer interface {
	ConsumeUnreadInAppForUser(ctx context.Context, userID string) ([]Notification, error)
}

// InAppHandler lists the bearer token's authorized user's unread InApp
// notifications and marks them read. This is the only channel with a
// pull-based inbox - see UnreadConsumer.
type InAppHandler struct {
	validator SessionValidator
	consumer  UnreadConsumer
}

func NewInAppHandler(validator SessionValidator, consumer UnreadConsumer) *InAppHandler {
	return &InAppHandler{validator: validator, consumer: consumer}
}

// ConsumeUnread serves GET /notifications/inapp/unread.
func (h *InAppHandler) ConsumeUnread(c echo.Context) error {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
	}
	userID, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	notifications, err := h.consumer.ConsumeUnreadInAppForUser(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list unread notifications")
	}

	out := make([]notificationResponse, 0, len(notifications))
	for i := range notifications {
		out = append(out, toNotificationResponse(&notifications[i]))
	}
	return c.JSON(http.StatusOK, out)
}

var _ UnreadConsumer = (*NotificationService)(nil)
