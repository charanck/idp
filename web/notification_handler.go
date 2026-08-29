package web

import (
	"context"
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
			Status:          n.Status,
			Attempt:         n.Attempt,
			Provider:        providerName,
			Error:           errMsg,
			CreatedAt:       n.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:       n.UpdatedAt.Format("2006-01-02 15:04:05"),
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

var _ NotificationStore = (*notification.NotificationService)(nil)
