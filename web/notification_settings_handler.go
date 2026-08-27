package web

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/datatypes"

	"controlplane/internal/notification"
	"controlplane/web/template/pages"
)

// ProviderSettingStore is what notification-settings CRUD handlers need.
// Satisfied by *notification.ProviderSettingService.
type ProviderSettingStore interface {
	Get(ctx context.Context, channel string) (*notification.ProviderSetting, error)
	List(ctx context.Context) ([]notification.ProviderSetting, error)
	Upsert(ctx context.Context, in notification.UpsertInput) (*notification.ProviderSetting, error)
}

type NotificationSettingsHandler struct {
	settings ProviderSettingStore
	activity ActivityRecorder
}

func NewNotificationSettingsHandler(settings ProviderSettingStore, activity ActivityRecorder) *NotificationSettingsHandler {
	return &NotificationSettingsHandler{settings: settings, activity: activity}
}

var notificationChannelLabels = []struct {
	Channel string
	Label   string
}{
	{notification.ChannelEmail, "Email"},
	{notification.ChannelSMS, "SMS"},
	{notification.ChannelWhatsApp, "WhatsApp"},
}

func notificationChannelLabel(channel string) string {
	for _, c := range notificationChannelLabels {
		if c.Channel == channel {
			return c.Label
		}
	}
	return ""
}

// List shows a fixed 3-row list (email/sms/whatsapp) with only a
// "configured"/"not configured" indicator - never the decrypted
// credentials, mirroring ConfigService's "***ENCRYPTED***" convention.
func (h *NotificationSettingsHandler) List(c echo.Context) error {
	settings, err := h.settings.List(c.Request().Context())
	if err != nil {
		return err
	}
	configuredByChannel := make(map[string]bool, len(settings))
	activeByChannel := make(map[string]bool, len(settings))
	for _, s := range settings {
		configuredByChannel[s.Channel] = s.Credentials != ""
		activeByChannel[s.Channel] = s.IsActive
	}

	rows := make([]pages.NotificationSettingRow, 0, len(notificationChannelLabels))
	for _, entry := range notificationChannelLabels {
		rows = append(rows, pages.NotificationSettingRow{
			Channel:    entry.Channel,
			Label:      entry.Label,
			Configured: configuredByChannel[entry.Channel],
			IsActive:   activeByChannel[entry.Channel],
		})
	}

	return pages.NotificationSettingsList(flashes(c), navUser(c), pages.NotificationSettingsListData{
		Rows: rows, CSRFToken: csrfToken(c),
	}).Render(c.Request().Context(), c.Response())
}

func (h *NotificationSettingsHandler) Edit(c echo.Context) error {
	channel := c.Param("channel")
	label := notificationChannelLabel(channel)
	if label == "" {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	existing, err := h.settings.Get(c.Request().Context(), channel)
	if err != nil {
		return err
	}
	configured := existing != nil && existing.Credentials != ""

	if c.Request().Method == http.MethodGet {
		data := pages.NotificationSettingFormData{
			CSRFToken: csrfToken(c), Channel: channel, Label: label, Configured: configured,
		}
		if existing != nil {
			data.Config = string(existing.Config)
			data.IsActive = existing.IsActive
		}
		return pages.NotificationSettingForm(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
	}

	configJSON := c.FormValue("config")
	if configJSON == "" {
		configJSON = "{}"
	}
	reRender := func(errMsg string) error {
		return pages.NotificationSettingForm(flashes(c), navUser(c), pages.NotificationSettingFormData{
			CSRFToken: csrfToken(c), Channel: channel, Label: label,
			Config: configJSON, IsActive: c.FormValue("is_active") != "",
			Configured: configured, Error: errMsg,
		}).Render(c.Request().Context(), c.Response())
	}

	if !json.Valid([]byte(configJSON)) {
		return reRender("Config must be valid JSON.")
	}

	updated, err := h.settings.Upsert(c.Request().Context(), notification.UpsertInput{
		Channel:     channel,
		Config:      datatypes.JSON(configJSON),
		Credentials: c.FormValue("credentials"),
		IsActive:    c.FormValue("is_active") != "",
	})
	if err != nil {
		return reRender("Failed to save settings: " + err.Error())
	}

	h.activity.LogUpdate(requestContext(c), "notification_provider_settings", updated.ID.String(), label, map[string]any{"is_active": updated.IsActive})
	AddFlash(c, "success", label+" notification settings saved.")
	return c.Redirect(http.StatusFound, "/notification-settings/")
}

var _ ProviderSettingStore = (*notification.ProviderSettingService)(nil)
