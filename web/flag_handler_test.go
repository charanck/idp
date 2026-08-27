package web_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/config"
	"controlplane/web"
)

func newFlagHandlerFixture() (*fakeFlagStore, *fakeApplicationStore, *fakeEnvironmentStore, *fakeActivityRecorder, *web.FlagHandler) {
	flags := newFakeFlagStore()
	apps := newFakeApplicationStore()
	envs := newFakeEnvironmentStore()
	activity := &fakeActivityRecorder{}
	h := web.NewFlagHandler(flags, apps, envs, activity)
	return flags, apps, envs, activity, h
}

func TestFlagListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	flags, apps, _, _, h := newFlagHandlerFixture()
	app := apps.put(config.Application{Name: "app-a"})
	flags.put(config.FeatureFlag{ApplicationID: app.ID, Name: "flag-a", Application: config.Application{Name: "app-a"}})

	rec := callHandler(t, store, http.MethodGet, "/flags/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestFlagCreateHandler_UnknownApplicationShowsError(t *testing.T) {
	store := newSessionStore(t)
	_, _, _, activity, h := newFlagHandlerFixture()

	form := url.Values{"application_id": {uuid.New().String()}, "name": {"new-flag"}}
	rec := callHandler(t, store, http.MethodPost, "/flags/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

func TestFlagToggleHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, _, _, h := newFlagHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodPost, "/flags/"+unknown+"/toggle/",
		map[string]string{"id": unknown}, nil, nil, h.Toggle)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestFlagToggleHandler_TogglesEnabledState(t *testing.T) {
	store := newSessionStore(t)
	flags, _, _, activity, h := newFlagHandlerFixture()
	fl := flags.put(config.FeatureFlag{Name: "flag-a", IsEnabled: false, Application: config.Application{Name: "a"}, Environment: config.Environment{Name: "e"}})
	id := fl.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/flags/"+id+"/toggle/",
		map[string]string{"id": id}, nil, nil, h.Toggle)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	updated, _ := flags.GetFlagByID(context.Background(), fl.ID)
	if updated == nil || !updated.IsEnabled {
		t.Fatalf("flag not toggled to enabled: %+v", updated)
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestFlagDeleteHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, _, _, h := newFlagHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/flags/"+unknown+"/delete/",
		map[string]string{"id": unknown}, nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
