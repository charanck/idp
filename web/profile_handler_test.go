package web_test

import (
	"net/http"
	"net/url"
	"testing"

	authmodel "controlplane/internal/model/auth"
	"controlplane/web"
)

func newProfileHandlerFixture() (*fakeProfileStore, *web.ProfileHandler) {
	profile := newFakeProfileStore()
	h := web.NewProfileHandler(profile)
	return profile, h
}

func TestProfileHandler_ShowRendersCurrentUser(t *testing.T) {
	store := newSessionStore(t)
	profile, h := newProfileHandlerFixture()
	user := profile.put(authmodel.User{Email: "alice@example.com", Username: "alice", FirstName: "Alice"})

	rec := callHandler(t, store, http.MethodGet, "/profile/", nil, &user, h.Show)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestProfileHandler_UpdatesIdentityFields(t *testing.T) {
	store := newSessionStore(t)
	profile, h := newProfileHandlerFixture()
	user := profile.put(authmodel.User{Email: "alice@example.com", Username: "alice"})

	form := url.Values{"username": {"alice2"}, "first_name": {"Alice"}, "last_name": {"Anderson"}}
	rec := callHandler(t, store, http.MethodPost, "/profile/", form, &user, h.Show)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	updated := profile.users[user.ID]
	if updated.Username != "alice2" || updated.FirstName != "Alice" || updated.LastName != "Anderson" {
		t.Fatalf("unexpected user after update: %+v", updated)
	}
}

func TestProfileHandler_EmptyUsernameShowsError(t *testing.T) {
	store := newSessionStore(t)
	profile, h := newProfileHandlerFixture()
	user := profile.put(authmodel.User{Email: "alice@example.com", Username: "alice"})

	form := url.Values{"username": {""}, "first_name": {"Alice"}}
	rec := callHandler(t, store, http.MethodPost, "/profile/", form, &user, h.Show)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
