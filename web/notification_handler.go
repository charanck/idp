package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	notificationmodel "controlplane/internal/model/notification"
	"controlplane/internal/notification"
	"controlplane/web/template/pages"
)

const notificationsPageSize = 25

// NotificationStore is the read side of notification delivery, used by the
// (all-logged-in-users, read-only) notifications page. Satisfied by
// *notification.NotificationService.
type NotificationStore interface {
	ListNotifications(ctx context.Context, filter notificationmodel.ListNotificationsFilter) ([]notificationmodel.Notification, error)
	GetNotification(ctx context.Context, id uuid.UUID) (*notificationmodel.Notification, error)
}

// notificationRecipientSummary best-effort extracts a human-readable
// recipient string ("email"/"phone"/"user_id", whichever is present) from a
// notification's raw jsonb Recipient blob, for display in the list without
// requiring the full JSON detail view.
func notificationRecipientSummary(recipient []byte) string {
	var fields map[string]any
	if err := json.Unmarshal(recipient, &fields); err != nil {
		return "-"
	}
	for _, key := range []string{"email", "phone", "user_id"} {
		if v, ok := fields[key].(string); ok && v != "" {
			return v
		}
	}
	return "-"
}

// prettyJSON indents raw jsonb bytes for display; falls back to the raw
// string if it isn't valid JSON (shouldn't happen for jsonb columns, but the
// detail page must never fail to render over it).
func prettyJSON(raw []byte) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return string(raw)
	}
	return indented.String()
}

type NotificationHandler struct {
	notifications NotificationStore
	apps          ApplicationStore
}

func NewNotificationHandler(notifications NotificationStore, apps ApplicationStore) *NotificationHandler {
	return &NotificationHandler{notifications: notifications, apps: apps}
}

func (h *NotificationHandler) List(c echo.Context) error {
	appIDFilter := c.QueryParam("application_id")
	channelFilter := c.QueryParam("channel")
	statusFilter := c.QueryParam("status")

	filter := notificationmodel.ListNotificationsFilter{Channel: channelFilter, Status: statusFilter}
	if appIDFilter != "" {
		if id, err := uuid.Parse(appIDFilter); err == nil {
			filter.ApplicationID = &id
		}
	}

	notifications, err := h.notifications.ListNotifications(c.Request().Context(), filter)
	if err != nil {
		return err
	}

	rows := make([]pages.NotificationRow, 0, len(notifications))
	for _, n := range notifications {
		errMsg := ""
		if n.Error != nil {
			errMsg = *n.Error
		}
		providerName := ""
		if n.Provider != nil {
			providerName = *n.Provider
		}
		rows = append(rows, pages.NotificationRow{
			ID:              n.ID.String(),
			ApplicationName: n.Application.Name,
			Channel:         n.Channel,
			Recipient:       notificationRecipientSummary(n.Recipient),
			Status:          n.Status,
			Attempt:         n.Attempt,
			Provider:        providerName,
			Error:           errMsg,
			CreatedAt:       n.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:       n.UpdatedAt.Format("2006-01-02 15:04:05"),
			DetailURL:       "/notifications/" + n.ID.String() + "/",
		})
	}

	apps, err := listApplications(c.Request().Context(), h.apps)
	if err != nil {
		return err
	}

	page := Paginate(rows, notificationsPageSize, PageParam(c))

	extra := url.Values{}
	if appIDFilter != "" {
		extra.Set("application_id", appIDFilter)
	}
	if channelFilter != "" {
		extra.Set("channel", channelFilter)
	}
	if statusFilter != "" {
		extra.Set("status", statusFilter)
	}

	return pages.NotificationList(flashes(c), navUser(c), pages.NotificationListData{
		Notifications: page.Items, Applications: apps,
		CurrentAppID: appIDFilter, CurrentChannel: channelFilter, CurrentStatus: statusFilter,
		Page: page.Number, NumPages: page.NumPages,
		HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(), ExtraQuery: extra.Encode(),
	}).Render(c.Request().Context(), c.Response())
}

func (h *NotificationHandler) Detail(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	n, err := h.notifications.GetNotification(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if n == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	errMsg := ""
	if n.Error != nil {
		errMsg = *n.Error
	}
	providerName := ""
	if n.Provider != nil {
		providerName = *n.Provider
	}
	providerMessageID := ""
	if n.ProviderMessageID != nil {
		providerMessageID = *n.ProviderMessageID
	}
	idempotencyKey := ""
	if n.IdempotencyKey != nil {
		idempotencyKey = *n.IdempotencyKey
	}
	readAt := ""
	if n.ReadAt != nil {
		readAt = n.ReadAt.Format("2006-01-02 15:04:05")
	}

	return pages.NotificationDetail(flashes(c), navUser(c), pages.NotificationDetailData{
		ID:                n.ID.String(),
		ApplicationName:   n.Application.Name,
		Channel:           n.Channel,
		Status:            n.Status,
		Attempt:           n.Attempt,
		Provider:          providerName,
		ProviderMessageID: providerMessageID,
		IdempotencyKey:    idempotencyKey,
		Error:             errMsg,
		Recipient:         prettyJSON(n.Recipient),
		Content:           prettyJSON(n.Content),
		ReadAt:            readAt,
		CreatedAt:         n.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:         n.UpdatedAt.Format("2006-01-02 15:04:05"),
	}).Render(c.Request().Context(), c.Response())
}

var _ NotificationStore = (*notification.NotificationService)(nil)
