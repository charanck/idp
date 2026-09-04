package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"gorm.io/datatypes"

	notificationmodel "controlplane/internal/model/notification"
	"controlplane/internal/notification"
	emailprovider "controlplane/internal/notification/provider"
	"controlplane/web/template/pages"
)

// ProviderSettingStore is what notification-settings CRUD handlers need.
// Satisfied by *notification.ProviderSettingService.
type ProviderSettingStore interface {
	Get(ctx context.Context, channel string) (*notificationmodel.ProviderSetting, error)
	List(ctx context.Context) ([]notificationmodel.ProviderSetting, error)
	Upsert(ctx context.Context, in notification.UpsertInput) (*notificationmodel.ProviderSetting, error)
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
	{notificationmodel.ChannelEmail, "Email"},
	{notificationmodel.ChannelSMS, "SMS"},
}

func notificationChannelLabel(channel string) string {
	for _, c := range notificationChannelLabels {
		if c.Channel == channel {
			return c.Label
		}
	}
	return ""
}

// List shows a fixed 2-row list (email/sms) with only a
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

	if channel == notificationmodel.ChannelEmail {
		return h.editEmail(c, existing, configured, label)
	}

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

// editEmail handles GET/POST for the "email" channel's structured SMTP
// settings form - distinct from the generic raw-JSON path other channels
// use, since email is the one channel with a real (SMTP) implementation.
func (h *NotificationSettingsHandler) editEmail(c echo.Context, existing *notificationmodel.ProviderSetting, configured bool, label string) error {
	if c.Request().Method == http.MethodGet {
		data := pages.NotificationSettingFormData{
			CSRFToken: csrfToken(c), Channel: notificationmodel.ChannelEmail, Label: label, Configured: configured,
		}
		if existing != nil {
			data.IsActive = existing.IsActive
			var cfg emailprovider.EmailSMTPConfig
			if err := json.Unmarshal(existing.Config, &cfg); err == nil {
				data.EmailHost = cfg.Host
				if cfg.Port != 0 {
					data.EmailPort = strconv.Itoa(cfg.Port)
				}
				data.EmailFrom = cfg.From
				data.EmailFromName = cfg.FromName
				data.EmailTLSMode = string(cfg.TLSMode)
			}
		}
		return pages.NotificationSettingForm(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
	}

	host := strings.TrimSpace(c.FormValue("host"))
	portStr := strings.TrimSpace(c.FormValue("port"))
	from := strings.TrimSpace(c.FormValue("from"))
	fromName := strings.TrimSpace(c.FormValue("from_name"))
	tlsMode := c.FormValue("tls_mode")
	username := c.FormValue("username")
	password := c.FormValue("password")
	isActive := c.FormValue("is_active") != ""

	reRender := func(errMsg string) error {
		return pages.NotificationSettingForm(flashes(c), navUser(c), pages.NotificationSettingFormData{
			CSRFToken: csrfToken(c), Channel: notificationmodel.ChannelEmail, Label: label,
			IsActive: isActive, Configured: configured, Error: errMsg,
			EmailHost: host, EmailPort: portStr, EmailFrom: from, EmailFromName: fromName,
			EmailTLSMode: tlsMode, EmailUsername: username,
		}).Render(c.Request().Context(), c.Response())
	}

	if host == "" || portStr == "" || from == "" {
		return reRender("Host, port, and from address are required.")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return reRender("Port must be a valid number between 1 and 65535.")
	}
	switch emailprovider.EmailTLSMode(tlsMode) {
	case emailprovider.EmailTLSNone, emailprovider.EmailTLSStartTLS, emailprovider.EmailTLSImplicit:
	default:
		return reRender("TLS mode must be none, starttls, or tls.")
	}

	var credentialsJSON string
	switch {
	case username == "" && password == "":
		// Leave blank - Upsert's existing "blank Credentials means unchanged"
		// convention applies.
	case username == "" || password == "":
		return reRender("Username and password must both be set, or both left blank to keep the existing credentials.")
	default:
		encoded, err := json.Marshal(emailprovider.EmailSMTPCredentials{Username: username, Password: password})
		if err != nil {
			return reRender("Failed to encode credentials.")
		}
		credentialsJSON = string(encoded)
	}

	cfgJSON, err := json.Marshal(emailprovider.EmailSMTPConfig{
		Provider: emailprovider.EmailProviderSMTP,
		Host:     host,
		Port:     port,
		From:     from,
		FromName: fromName,
		TLSMode:  emailprovider.EmailTLSMode(tlsMode),
	})
	if err != nil {
		return reRender("Failed to encode config.")
	}

	updated, err := h.settings.Upsert(c.Request().Context(), notification.UpsertInput{
		Channel:     notificationmodel.ChannelEmail,
		Config:      datatypes.JSON(cfgJSON),
		Credentials: credentialsJSON,
		IsActive:    isActive,
	})
	if err != nil {
		return reRender("Failed to save settings: " + err.Error())
	}

	h.activity.LogUpdate(requestContext(c), "notification_provider_settings", updated.ID.String(), label, map[string]any{"is_active": updated.IsActive})
	AddFlash(c, "success", label+" notification settings saved.")
	return c.Redirect(http.StatusFound, "/notification-settings/")
}

var _ ProviderSettingStore = (*notification.ProviderSettingService)(nil)
