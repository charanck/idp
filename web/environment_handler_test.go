package web_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/config"
	"controlplane/web"
)

func TestEnvironmentListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	apps := newFakeApplicationStore()
	app := apps.put(config.Application{Name: "app-a"})
	envs := newFakeEnvironmentStore()
	envs.put(config.Environment{ApplicationID: app.ID, Name: "prod"})
	activity := &fakeActivityRecorder{}
	h := web.NewEnvironmentHandler(envs, apps, activity)

	rec := callHandler(t, store, http.MethodGet, "/environments/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestEnvironmentCreateHandler_DuplicateNameShowsError(t *testing.T) {
	store := newSessionStore(t)
	apps := newFakeApplicationStore()
	app := apps.put(config.Application{Name: "app-a"})
	envs := newFakeEnvironmentStore()
	envs.put(config.Environment{ApplicationID: app.ID, Name: "prod"})
	activity := &fakeActivityRecorder{}
	h := web.NewEnvironmentHandler(envs, apps, activity)

	form := url.Values{"application_id": {app.ID.String()}, "name": {"prod"}}
	rec := callHandler(t, store, http.MethodPost, "/environments/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

func TestEnvironmentDeleteHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	envs := newFakeEnvironmentStore()
	apps := newFakeApplicationStore()
	activity := &fakeActivityRecorder{}
	h := web.NewEnvironmentHandler(envs, apps, activity)

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/environments/"+unknown+"/delete/",
		map[string]string{"id": unknown}, nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
