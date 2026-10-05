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
	GetUnreadInAppForUser(ctx context.Context, userID string, applicationID uuid.UUID) ([]notificationmodel.Notification, error)
	ConsumeUnreadInAppForUser(ctx context.Context, userID string, applicationID uuid.UUID) ([]notificationmodel.Notification, error)
	MarkInAppAsRead(ctx context.Context, notificationIDs []uuid.UUID) error
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

// GetUnread serves GET /notifications/inapp/unread.
func (h *InAppHandler) GetUnread(c echo.Context) error {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
	}
	claims, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	notifications, err := h.consumer.GetUnreadInAppForUser(c.Request().Context(), claims.UserID, claims.ApplicationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list unread notifications")
	}

	out := make([]notificationResponse, 0, len(notifications))
	for i := range notifications {
		out = append(out, toNotificationResponse(&notifications[i]))
	}
	return c.JSON(http.StatusOK, out)
}

type markAsReadRequest struct {
	NotificationIDs []string `json:"notification_ids"`
}

// MarkAsRead serves POST /notifications/inapp/mark-as-read.
func (h *InAppHandler) MarkAsRead(c echo.Context) error {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
	}
	_, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	var req markAsReadRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if len(req.NotificationIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "notification_ids is required")
	}

	notificationIDs := make([]uuid.UUID, 0, len(req.NotificationIDs))
	for _, id := range req.NotificationIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid notification id format")
		}
		notificationIDs = append(notificationIDs, parsed)
	}

	if err := h.consumer.MarkInAppAsRead(c.Request().Context(), notificationIDs); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to mark notifications as read")
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

var _ InAppConsumer = (*notification.NotificationService)(nil)
