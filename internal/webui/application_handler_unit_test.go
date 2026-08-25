package webui_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"controlplane/internal/config"
	"controlplane/internal/webui"
)

func TestApplicationListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	apps := newFakeApplicationStore()
	apps.put(config.Application{Name: "svc-a"})
	activity := &fakeActivityRecorder{}
	h := webui.NewApplicationHandler(apps, activity)

	rec := callHandler(t, store, http.MethodGet, "/applications/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestApplicationCreateHandler_PersistsAndRedirects(t *testing.T) {
	store := newSessionStore(t)
	apps := newFakeApplicationStore()
	activity := &fakeActivityRecorder{}
	h := webui.NewApplicationHandler(apps, activity)

	form := url.Values{"name": {"new-app"}}
	rec := callHandler(t, store, http.MethodPost, "/applications/create/", form, nil, h.Create)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	all, _ := apps.ListAllApplications(context.Background(), "")
	if len(all) != 1 || all[0].Name != "new-app" {
		t.Fatalf("apps = %+v, want single new-app", all)
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestApplicationCreateHandler_DuplicateNameShowsError(t *testing.T) {
	store := newSessionStore(t)
	apps := newFakeApplicationStore()
	apps.put(config.Application{Name: "dup"})
	activity := &fakeActivityRecorder{}
	h := webui.NewApplicationHandler(apps, activity)

	form := url.Values{"name": {"dup"}}
	rec := callHandler(t, store, http.MethodPost, "/applications/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

func TestApplicationDeleteHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	apps := newFakeApplicationStore()
	activity := &fakeActivityRecorder{}
	h := webui.NewApplicationHandler(apps, activity)

	rec := callHandler(t, store, http.MethodGet, "/applications/not-a-uuid/delete/", nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
