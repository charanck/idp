package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	authmodel "controlplane/internal/model/auth"
	configmodel "controlplane/internal/model/config"
	"controlplane/internal/security"
	"controlplane/web"
)

func newConfigHandlerFixture() (*fakeConfigStore, *fakeEnvironmentStore, *fakeApplicationStore, *fakeActivityRecorder, *web.ConfigHandler) {
	return newConfigHandlerFixtureWithLimiter(fakeRateLimiter{})
}

func newConfigHandlerFixtureWithLimiter(limiter fakeRateLimiter) (*fakeConfigStore, *fakeEnvironmentStore, *fakeApplicationStore, *fakeActivityRecorder, *web.ConfigHandler) {
	configs := newFakeConfigStore()
	envs := newFakeEnvironmentStore()
	apps := newFakeApplicationStore()
	activity := &fakeActivityRecorder{}
	h := web.NewConfigHandler(configs, envs, apps, activity, limiter, 10, 60)
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
	configs.put(configmodel.ConfigEntry{
		ApplicationID: appID, EnvironmentID: envAID, Key: "DATABASE_URL", Value: "v1",
		Application: configmodel.Application{ID: appID, Name: "app"}, Environment: configmodel.Environment{ID: envAID, Name: "staging"},
	})
	configs.put(configmodel.ConfigEntry{
		ApplicationID: appID, EnvironmentID: envBID, Key: "DATABASE_URL", Value: "v2",
		Application: configmodel.Application{ID: appID, Name: "app"}, Environment: configmodel.Environment{ID: envBID, Name: "prod"},
	})
	configs.put(configmodel.ConfigEntry{
		ApplicationID: appID, EnvironmentID: envAID, Key: "OTHER_KEY", Value: "v3",
		Application: configmodel.Application{ID: appID, Name: "app"}, Environment: configmodel.Environment{ID: envAID, Name: "staging"},
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

	entry := configs.put(configmodel.ConfigEntry{Key: "K", Application: configmodel.Application{Name: "a"}, Environment: configmodel.Environment{Name: "e"}})
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

func TestConfigRevealHandler_NonSecretReturns404(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, _, h := newConfigHandlerFixture()

	entry := configs.put(configmodel.ConfigEntry{
		Key: "K", IsSecret: false,
		Application: configmodel.Application{Name: "a"}, Environment: configmodel.Environment{Name: "e"},
	})
	id := entry.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/configs/"+id+"/reveal/",
		map[string]string{"id": id}, nil, nil, h.Reveal)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestConfigRevealHandler_GetRendersPasswordPrompt(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, _, h := newConfigHandlerFixture()

	entry := configs.put(configmodel.ConfigEntry{
		Key: "K", IsSecret: true, Value: "top-secret",
		Application: configmodel.Application{Name: "a"}, Environment: configmodel.Environment{Name: "e"},
	})
	id := entry.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/configs/"+id+"/reveal/",
		map[string]string{"id": id}, nil, nil, h.Reveal)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "top-secret") {
		t.Fatal("password prompt must not leak the secret value")
	}
	if !strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("expected a password prompt, body=%s", rec.Body.String())
	}
}

func TestConfigRevealHandler_WrongPasswordShowsErrorAndDoesNotLeakValue(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, activity, h := newConfigHandlerFixture()

	hash, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &authmodel.User{ID: uuid.New(), Email: "dev@example.com", Password: hash}

	entry := configs.put(configmodel.ConfigEntry{
		Key: "K", IsSecret: true, Value: "top-secret",
		Application: configmodel.Application{Name: "a"}, Environment: configmodel.Environment{Name: "e"},
	})
	id := entry.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/configs/"+id+"/reveal/",
		map[string]string{"id": id}, url.Values{"password": {"wrong-password"}}, user, h.Reveal)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "top-secret") {
		t.Fatal("wrong password must not leak the secret value")
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 (no read logged on failed reveal)", activity.count())
	}
}

func TestConfigRevealHandler_RateLimitedShowsError(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, _, h := newConfigHandlerFixtureWithLimiter(fakeRateLimiter{limited: true})

	user := &authmodel.User{ID: uuid.New(), Email: "dev@example.com", Password: "irrelevant"}
	entry := configs.put(configmodel.ConfigEntry{
		Key: "K", IsSecret: true, Value: "top-secret",
		Application: configmodel.Application{Name: "a"}, Environment: configmodel.Environment{Name: "e"},
	})
	id := entry.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/configs/"+id+"/reveal/",
		map[string]string{"id": id}, url.Values{"password": {"anything"}}, user, h.Reveal)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "top-secret") {
		t.Fatal("rate-limited reveal must not leak the secret value")
	}
	if !strings.Contains(rec.Body.String(), "Too many attempts") {
		t.Fatalf("expected a rate-limit error, body=%s", rec.Body.String())
	}
}

func TestConfigRevealHandler_CorrectPasswordReturnsValueAndLogsRead(t *testing.T) {
	store := newSessionStore(t)
	configs, _, _, activity, h := newConfigHandlerFixture()

	hash, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &authmodel.User{ID: uuid.New(), Email: "dev@example.com", Password: hash}

	entry := configs.put(configmodel.ConfigEntry{
		Key: "K", IsSecret: true, Value: "top-secret",
		Application: configmodel.Application{Name: "a"}, Environment: configmodel.Environment{Name: "e"},
	})
	id := entry.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/configs/"+id+"/reveal/",
		map[string]string{"id": id}, url.Values{"password": {"correct-password"}}, user, h.Reveal)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "top-secret") {
		t.Fatalf("expected the decrypted value in the response, body=%s", rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
	if activity.lastCall() != "read:config" {
		t.Fatalf("activity.lastCall() = %q, want read:config", activity.lastCall())
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
