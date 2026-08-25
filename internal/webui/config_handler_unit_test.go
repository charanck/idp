package webui_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/config"
	"controlplane/internal/webui"
)

func newConfigHandlerFixture() (*fakeConfigStore, *fakeEnvironmentStore, *fakeApplicationStore, *fakeActivityRecorder, *webui.ConfigHandler) {
	configs := newFakeConfigStore()
	envs := newFakeEnvironmentStore()
	apps := newFakeApplicationStore()
	activity := &fakeActivityRecorder{}
	h := webui.NewConfigHandler(configs, envs, apps, activity)
	return configs, envs, apps, activity, h
}

// TestConfigsListHandler_GroupsEntriesByKey confirms List groups config
// entries sharing the same (application, key, is_secret, type) into a single
// pages.ConfigGroup row with one entry per environment, rather than one row
// per ConfigEntry.
func TestConfigsListHandler_GroupsEntriesByKey(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, _, h := newConfigHandlerFixture()

	appID := uuid.New()
	envAID := uuid.New()
	envBID := uuid.New()
	configs.put(config.ConfigEntry{
		ApplicationID: appID, EnvironmentID: envAID, Key: "DATABASE_URL", Value: "v1",
		Application: config.Application{ID: appID, Name: "app"}, Environment: config.Environment{ID: envAID, Name: "staging"},
	})
	configs.put(config.ConfigEntry{
		ApplicationID: appID, EnvironmentID: envBID, Key: "DATABASE_URL", Value: "v2",
		Application: config.Application{ID: appID, Name: "app"}, Environment: config.Environment{ID: envBID, Name: "prod"},
	})
	configs.put(config.ConfigEntry{
		ApplicationID: appID, EnvironmentID: envAID, Key: "OTHER_KEY", Value: "v3",
		Application: config.Application{ID: appID, Name: "app"}, Environment: config.Environment{ID: envAID, Name: "staging"},
	})

	rec := callHandler(t, store, http.MethodGet, "/configs/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if got := countOccurrences(body, "DATABASE_URL"); got != 1 {
		t.Fatalf("DATABASE_URL should be rendered once as a single group, occurred %d times", got)
	}
}

func TestConfigDeleteHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, _, _, h := newConfigHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/configs/"+unknown+"/delete/",
		map[string]string{"id": unknown}, nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestConfigDeleteHandler_MalformedIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, _, _, h := newConfigHandlerFixture()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/configs/not-a-uuid/delete/",
		map[string]string{"id": "not-a-uuid"}, nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestConfigDeleteHandler_DeletesAndRedirects(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, activity, h := newConfigHandlerFixture()

	entry := configs.put(config.ConfigEntry{Key: "K", Application: config.Application{Name: "a"}, Environment: config.Environment{Name: "e"}})
	id := entry.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/configs/"+id+"/delete/",
		map[string]string{"id": id}, nil, nil, h.Delete)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func countOccurrences(haystack, needle string) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			count++
			i += len(needle) - 1
		}
	}
	return count
}
