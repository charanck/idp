package http

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	notificationmodel "controlplane/internal/model/notification"
	"controlplane/internal/notification"
)

// InAppConsumer is the narrow slice of *notification.NotificationService
// that InAppHandler needs.
type InAppConsumer interface {
	ListInAppForUser(ctx context.Context, userID string, applicationID uuid.UUID) ([]notificationmodel.Notification, error)
	ConsumeUnreadInAppForUser(ctx context.Context, userID string, applicationID uuid.UUID) ([]notificationmodel.Notification, error)
}

// InAppHandler lists the bearer token's authorized user's in-app
// notifications. The read-state-changing /unread variant is kept separate.
type InAppHandler struct {
	validator SessionValidator
	consumer  InAppConsumer
}

func NewInAppHandler(validator SessionValidator, consumer InAppConsumer) *InAppHandler {
	return &InAppHandler{validator: validator, consumer: consumer}
}

// List serves GET /notifications/inapp.
func (h *InAppHandler) List(c echo.Context) error {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
	}
	claims, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	notifications, err := h.consumer.ListInAppForUser(c.Request().Context(), claims.UserID, claims.ApplicationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list in-app notifications")
	}

	out := make([]notificationResponse, 0, len(notifications))
	for i := range notifications {
		out = append(out, toNotificationResponse(&notifications[i]))
	}
	return c.JSON(http.StatusOK, out)
}

// ConsumeUnread serves GET /notifications/inapp/unread.
func (h *InAppHandler) ConsumeUnread(c echo.Context) error {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
	}
	claims, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	notifications, err := h.consumer.ConsumeUnreadInAppForUser(c.Request().Context(), claims.UserID, claims.ApplicationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list unread notifications")
	}

	out := make([]notificationResponse, 0, len(notifications))
	for i := range notifications {
		out = append(out, toNotificationResponse(&notifications[i]))
	}
	return c.JSON(http.StatusOK, out)
}

var _ InAppConsumer = (*notification.NotificationService)(nil)
