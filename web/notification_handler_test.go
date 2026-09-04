package web_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	notificationmodel "controlplane/internal/model/notification"
	"controlplane/web"
)

func TestNotificationListHandler_NoFiltersReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	notifications := &fakeNotificationStore{notifications: []notificationmodel.Notification{
		{ID: uuid.New(), Channel: "email", Status: notificationmodel.StatusSent},
		{ID: uuid.New(), Channel: "inapp", Status: notificationmodel.StatusQueued},
	}}
	apps := newFakeApplicationStore()
	h := web.NewNotificationHandler(notifications, apps)

	rec := callHandler(t, store, http.MethodGet, "/notifications/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationListHandler_FiltersByChannelAndStatus(t *testing.T) {
	store := newSessionStore(t)
	notifications := &fakeNotificationStore{notifications: []notificationmodel.Notification{
		{ID: uuid.New(), Channel: "email", Status: notificationmodel.StatusSent},
		{ID: uuid.New(), Channel: "inapp", Status: notificationmodel.StatusQueued},
	}}
	apps := newFakeApplicationStore()
	h := web.NewNotificationHandler(notifications, apps)

	rec := callHandler(t, store, http.MethodGet, "/notifications/?channel=email&status=sent", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationDetailHandler_ReturnsOKForKnownID(t *testing.T) {
	store := newSessionStore(t)
	id := uuid.New()
	notifications := &fakeNotificationStore{notifications: []notificationmodel.Notification{
		{ID: id, Channel: "email", Status: notificationmodel.StatusSent},
	}}
	apps := newFakeApplicationStore()
	h := web.NewNotificationHandler(notifications, apps)

	rec := callHandlerWithParams(t, store, http.MethodGet, "/notifications/"+id.String()+"/",
		map[string]string{"id": id.String()}, nil, nil, h.Detail)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationDetailHandler_ReturnsNotFoundForUnknownID(t *testing.T) {
	store := newSessionStore(t)
	notifications := &fakeNotificationStore{}
	apps := newFakeApplicationStore()
	h := web.NewNotificationHandler(notifications, apps)

	id := uuid.New()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/notifications/"+id.String()+"/",
		map[string]string{"id": id.String()}, nil, nil, h.Detail)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestNotificationListHandler_FiltersByApplicationID(t *testing.T) {
	store := newSessionStore(t)
	appID := uuid.New()
	notifications := &fakeNotificationStore{notifications: []notificationmodel.Notification{
		{ID: uuid.New(), ApplicationID: appID, Channel: "email", Status: notificationmodel.StatusSent},
		{ID: uuid.New(), ApplicationID: uuid.New(), Channel: "inapp", Status: notificationmodel.StatusQueued},
	}}
	apps := newFakeApplicationStore()
	h := web.NewNotificationHandler(notifications, apps)

	rec := callHandler(t, store, http.MethodGet, "/notifications/?application_id="+appID.String(), nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
