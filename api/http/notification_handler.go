package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/datatypes"

	notificationmodel "controlplane/internal/model/notification"
	"controlplane/internal/notification"
)

type createNotificationRequest struct {
	Channel        string          `json:"channel"`
	Recipient      json.RawMessage `json:"recipient"`
	Content        json.RawMessage `json:"content"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type notificationResponse struct {
	ID                string          `json:"id"`
	Channel           string          `json:"channel"`
	Recipient         json.RawMessage `json:"recipient"`
	Content           json.RawMessage `json:"content"`
	Status            string          `json:"status"`
	Provider          string          `json:"provider,omitempty"`
	ProviderMessageID string          `json:"provider_message_id,omitempty"`
	Attempt           int             `json:"attempt"`
	IdempotencyKey    string          `json:"idempotency_key,omitempty"`
	Error             string          `json:"error,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

func toNotificationResponse(n *notificationmodel.Notification) notificationResponse {
	resp := notificationResponse{
		ID:        n.ID.String(),
		Channel:   n.Channel,
		Recipient: json.RawMessage(n.Recipient),
		Content:   json.RawMessage(n.Content),
		Status:    n.Status,
		Attempt:   n.Attempt,
		CreatedAt: n.CreatedAt.Format(http.TimeFormat),
		UpdatedAt: n.UpdatedAt.Format(http.TimeFormat),
	}
	if n.Provider != nil {
		resp.Provider = *n.Provider
	}
	if n.ProviderMessageID != nil {
		resp.ProviderMessageID = *n.ProviderMessageID
	}
	if n.IdempotencyKey != nil {
		resp.IdempotencyKey = *n.IdempotencyKey
	}
	if n.Error != nil {
		resp.Error = *n.Error
	}
	return resp
}

// NotificationCreator is the narrow slice of *notification.NotificationService that
// NotificationHandler.Create needs.
type NotificationCreator interface {
	CreateNotification(ctx context.Context, input notification.CreateNotificationInput) (*notificationmodel.Notification, error)
}

// NotificationLister is the narrow slice of *notification.NotificationService that
// NotificationHandler.List needs.
type NotificationLister interface {
	ListNotifications(ctx context.Context, filter notificationmodel.ListNotificationsFilter) ([]notificationmodel.Notification, error)
}

// NotificationGetter is the narrow slice of *notification.NotificationService that
// NotificationHandler.Get needs.
type NotificationGetter interface {
	GetNotification(ctx context.Context, id uuid.UUID) (*notificationmodel.Notification, error)
}

// NotificationHandler serves the S2S notification create/list/get endpoints.
type NotificationHandler struct {
	creator  NotificationCreator
	lister   NotificationLister
	getter   NotificationGetter
	channels notification.ChannelRegistry
}

func NewNotificationHandler(creator NotificationCreator, lister NotificationLister, getter NotificationGetter, channels notification.ChannelRegistry) *NotificationHandler {
	return &NotificationHandler{creator: creator, lister: lister, getter: getter, channels: channels}
}

// Create serves POST /notifications.
func (h *NotificationHandler) Create(c echo.Context) error {
	var req createNotificationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	channel, ok := h.channels[req.Channel]
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "channel must be one of: email, sms, whatsapp, inapp")
	}
	if err := channel.Validate(req.Recipient, req.Content); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	n, err := h.creator.CreateNotification(c.Request().Context(), notification.CreateNotificationInput{
		Channel:        req.Channel,
		Recipient:      datatypes.JSON(req.Recipient),
		Content:        datatypes.JSON(req.Content),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		slog.Error("create notification failed", "err", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create notification")
	}
	return c.JSON(http.StatusCreated, toNotificationResponse(n))
}

// List serves GET /notifications.
func (h *NotificationHandler) List(c echo.Context) error {
	filter := notificationmodel.ListNotificationsFilter{
		Channel: c.QueryParam("channel"),
		Status:  c.QueryParam("status"),
	}
	notifications, err := h.lister.ListNotifications(c.Request().Context(), filter)
	if err != nil {
		slog.Error("list notifications failed", "err", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list notifications")
	}

	out := make([]notificationResponse, 0, len(notifications))
	for i := range notifications {
		out = append(out, toNotificationResponse(&notifications[i]))
	}
	return c.JSON(http.StatusOK, out)
}

// Get serves GET /notifications/:id.
func (h *NotificationHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid notification id")
	}

	n, err := h.getter.GetNotification(c.Request().Context(), id)
	if err != nil {
		slog.Error("get notification failed", "id", id, "err", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get notification")
	}
	if n == nil {
		return echo.NewHTTPError(http.StatusNotFound, "notification not found")
	}
	return c.JSON(http.StatusOK, toNotificationResponse(n))
}

var (
	_ NotificationCreator = (*notification.NotificationService)(nil)
	_ NotificationLister  = (*notification.NotificationService)(nil)
	_ NotificationGetter  = (*notification.NotificationService)(nil)
)
