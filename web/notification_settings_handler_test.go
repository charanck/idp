package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	notificationmodel "controlplane/internal/model/notification"
	"controlplane/web"
)

func newNotificationSettingsHandlerFixture() (*fakeProviderSettingStore, *fakeActivityRecorder, *web.NotificationSettingsHandler) {
	settings := newFakeProviderSettingStore()
	activity := &fakeActivityRecorder{}
	h := web.NewNotificationSettingsHandler(settings, activity)
	return settings, activity, h
}

func TestNotificationSettingsListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	settings, _, h := newNotificationSettingsHandlerFixture()
	settings.put(notificationmodel.ProviderSetting{Channel: notificationmodel.ChannelEmail, Credentials: "secret", IsActive: true})

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
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/sms/edit/",
		map[string]string{"channel": notificationmodel.ChannelSMS}, form, nil, h.Edit)

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
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/sms/edit/",
		map[string]string{"channel": notificationmodel.ChannelSMS}, form, nil, h.Edit)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestNotificationSettingsEditHandler_EmailGetPrefillsStructuredFields(t *testing.T) {
	store := newSessionStore(t)
	settings, _, h := newNotificationSettingsHandlerFixture()
	settings.put(notificationmodel.ProviderSetting{
		Channel:     notificationmodel.ChannelEmail,
		Config:      []byte(`{"provider":"smtp","host":"smtp.example.com","port":587,"from":"noreply@example.com","from_name":"Example","tls_mode":"starttls"}`),
		Credentials: "secret",
		IsActive:    true,
	})

	rec := callHandlerWithParams(t, store, http.MethodGet, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, nil, nil, h.Edit)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"smtp.example.com", "587", "noreply@example.com", "Example"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing prefilled value %q; body=%s", want, body)
		}
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("body must not include decrypted credentials; body=%s", body)
	}
}

func TestNotificationSettingsEditHandler_EmailPostRequiresHostPortFrom(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newNotificationSettingsHandlerFixture()

	form := url.Values{"host": {""}, "port": {""}, "from": {""}}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0", activity.count())
	}
}

func TestNotificationSettingsEditHandler_EmailPostRejectsInvalidPort(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newNotificationSettingsHandlerFixture()

	form := url.Values{"host": {"smtp.example.com"}, "port": {"not-a-number"}, "from": {"noreply@example.com"}, "tls_mode": {"none"}}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationSettingsEditHandler_EmailPostRejectsInvalidTLSMode(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newNotificationSettingsHandlerFixture()

	form := url.Values{"host": {"smtp.example.com"}, "port": {"587"}, "from": {"noreply@example.com"}, "tls_mode": {"bogus"}}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationSettingsEditHandler_EmailPostRejectsMismatchedCredentials(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newNotificationSettingsHandlerFixture()

	form := url.Values{
		"host": {"smtp.example.com"}, "port": {"587"}, "from": {"noreply@example.com"}, "tls_mode": {"none"},
		"username": {"user"}, "password": {""},
	}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationSettingsEditHandler_EmailPostSavesStructuredConfigAndCredentials(t *testing.T) {
	store := newSessionStore(t)
	settings, activity, h := newNotificationSettingsHandlerFixture()

	form := url.Values{
		"host": {"smtp.example.com"}, "port": {"587"}, "from": {"noreply@example.com"}, "from_name": {"Example"},
		"tls_mode": {"starttls"}, "username": {"user"}, "password": {"pass"}, "is_active": {"on"},
	}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}

	saved, err := settings.Get(context.Background(), notificationmodel.ChannelEmail)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if saved == nil {
		t.Fatal("expected settings to be saved")
	}
	if !strings.Contains(string(saved.Config), `"host":"smtp.example.com"`) {
		t.Fatalf("Config = %s", saved.Config)
	}
	if saved.Credentials != `{"username":"user","password":"pass"}` {
		t.Fatalf("Credentials = %s", saved.Credentials)
	}
}

func TestNotificationSettingsEditHandler_EmailPostBlankCredentialsPreservesExisting(t *testing.T) {
	store := newSessionStore(t)
	settings, _, h := newNotificationSettingsHandlerFixture()
	settings.put(notificationmodel.ProviderSetting{
		Channel:     notificationmodel.ChannelEmail,
		Config:      []byte(`{"provider":"smtp","host":"old.example.com","port":25,"from":"old@example.com","tls_mode":"none"}`),
		Credentials: `{"username":"old","password":"old"}`,
		IsActive:    true,
	})

	form := url.Values{
		"host": {"smtp.example.com"}, "port": {"587"}, "from": {"noreply@example.com"}, "tls_mode": {"none"},
		"username": {""}, "password": {""}, "is_active": {"on"},
	}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/notification-settings/email/edit/",
		map[string]string{"channel": notificationmodel.ChannelEmail}, form, nil, h.Edit)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}

	saved, err := settings.Get(context.Background(), notificationmodel.ChannelEmail)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if saved.Credentials != `{"username":"old","password":"old"}` {
		t.Fatalf("Credentials = %s, want existing credentials preserved", saved.Credentials)
	}
}
