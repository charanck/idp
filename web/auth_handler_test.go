package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	authmodel "controlplane/internal/model/auth"
	"controlplane/web"
)

func newAuthHandlerFixture(authStore *fakeAuthStore, limiter fakeRateLimiter) (*fakeActivityRecorder, *web.AuthHandler) {
	activity := &fakeActivityRecorder{}
	h := web.NewAuthHandler(authStore, fakeOAuthActiveLister{}, limiter, activity, 5, 60)
	return activity, h
}

// TestLoginHandler_RateLimitedShowsError confirms the rate-limit check runs
// before AuthenticateUser is even attempted, and renders the "too many
// attempts" error rather than an invalid-credentials error.
func TestLoginHandler_RateLimitedShowsError(t *testing.T) {
	store := newSessionStore(t)
	authStore := newFakeAuthStore()
	authStore.put(authmodel.User{Email: "user@example.com", Password: "irrelevant"})
	_, h := newAuthHandlerFixture(authStore, fakeRateLimiter{limited: true})

	form := url.Values{"username": {"user@example.com"}, "password": {"whatever"}}
	rec := callHandler(t, store, http.MethodPost, "/login/", form, nil, h.Login)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered login form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Too many login attempts") {
		t.Fatalf("body does not contain rate-limit error message: %s", rec.Body.String())
	}
}

func TestLoginHandler_InvalidCredentialsShowsError(t *testing.T) {
	store := newSessionStore(t)
	authStore := newFakeAuthStore()
	activity, h := newAuthHandlerFixture(authStore, fakeRateLimiter{limited: false})

	form := url.Values{"username": {"nobody@example.com"}, "password": {"whatever"}}
	rec := callHandler(t, store, http.MethodPost, "/login/", form, nil, h.Login)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered login form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1 (LogLoginFailed)", activity.count())
	}
}

func TestLoginHandler_AlreadyLoggedInRedirectsToDashboard(t *testing.T) {
	store := newSessionStore(t)
	authStore := newFakeAuthStore()
	user := authmodel.User{Email: "user@example.com"}
	_, h := newAuthHandlerFixture(authStore, fakeRateLimiter{limited: false})

	rec := callHandler(t, store, http.MethodGet, "/login/", nil, &user, h.Login)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/", got)
	}
}
