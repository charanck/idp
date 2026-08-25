package webui_test

import (
	"net/http"
	"net/url"
	"testing"

	"controlplane/internal/notification"
	"controlplane/internal/webui"
)

func newNotificationSettingsHandlerFixture() (*fakeProviderSettingStore, *fakeActivityRecorder, *webui.NotificationSettingsHandler) {
	settings := newFakeProviderSettingStore()
	activity := &fakeActivityRecorder{}
	h := webui.NewNotificationSettingsHandler(settings, activity)
	return settings, activity, h
}

func TestNotificationSettingsListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	settings, _, h := newNotificationSettingsHandlerFixture()
	settings.put(notification.ProviderSetting{Channel: notification.ChannelEmail, Credentials: "secret", IsActive: true})

	rec := callHandler(t, store, http.MethodGet, "/notification-settings/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationSettingsEditHandler_UnknownChannelReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newNotificationSettingsHandlerFixture()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/notification-settings/pigeon/edit/",
		map[string]string{"channel": "pigeon"}, nil, nil, h.Edit)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestNotificationSettingsEditHandler_InvalidJSONShowsError(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newNotificationSettingsHandlerFixture()

	form := url.Values{"config": {"{not-json"}, "credentials": {"secret"}}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notification.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on invalid config", activity.count())
	}
}

func TestNotificationSettingsEditHandler_SavesAndRedirects(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newNotificationSettingsHandlerFixture()

	form := url.Values{"config": {"{}"}, "credentials": {"secret"}, "is_active": {"on"}}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notification.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}
