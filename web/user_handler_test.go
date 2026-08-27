package web_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/auth"
	"controlplane/web"
)

func newUserHandlerFixture() (*fakeUserStore, *fakeActivityRecorder, *web.UserHandler) {
	users := newFakeUserStore()
	activity := &fakeActivityRecorder{}
	h := web.NewUserHandler(users, activity)
	return users, activity, h
}

func TestUserListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	users, _, h := newUserHandlerFixture()
	users.put(auth.User{Email: "a@example.com", Username: "a"})

	rec := callHandler(t, store, http.MethodGet, "/users/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestUserCreateHandler_PasswordMismatchShowsError(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newUserHandlerFixture()

	form := url.Values{
		"email": {"new@example.com"}, "username": {"newuser"},
		"password1": {"password123"}, "password2": {"different123"},
	}
	rec := callHandler(t, store, http.MethodPost, "/users/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

func TestUserCreateHandler_DuplicateEmailShowsError(t *testing.T) {
	store := newSessionStore(t)
	users, activity, h := newUserHandlerFixture()
	users.put(auth.User{Email: "dup@example.com", Username: "dup"})

	form := url.Values{
		"email": {"dup@example.com"}, "username": {"other"},
		"password1": {"password123"}, "password2": {"password123"},
	}
	rec := callHandler(t, store, http.MethodPost, "/users/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

// TestUserEditHandler_CannotEscalateOwnRole confirms that when an admin edits
// their own account and submits changed is_active/is_staff values, those two
// fields are silently reverted rather than applied.
func TestUserEditHandler_CannotEscalateOwnRole(t *testing.T) {
	store := newSessionStore(t)
	users, _, h := newUserHandlerFixture()
	self := users.put(auth.User{Email: "self@example.com", Username: "self", IsActive: true, IsStaff: true})
	id := self.ID.String()

	form := url.Values{
		"email": {"self@example.com"}, "username": {"self"},
		// omit is_active and is_staff entirely, i.e. attempt to revoke both.
	}
	rec := callHandlerWithParams(t, store, http.MethodPost, "/users/"+id+"/edit/",
		map[string]string{"id": id}, form, &self, h.Edit)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	updated, _ := users.GetUserByIDAny(context.Background(), self.ID)
	if updated == nil {
		t.Fatal("user not found after edit")
	}
	if !updated.IsActive || !updated.IsStaff {
		t.Fatalf("self-escalation revert failed: got IsActive=%v IsStaff=%v, want both true (unchanged)", updated.IsActive, updated.IsStaff)
	}
}

func TestUserEditHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newUserHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/users/"+unknown+"/edit/",
		map[string]string{"id": unknown}, nil, nil, h.Edit)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestUserDeleteHandler_CannotDeleteOwnAccount(t *testing.T) {
	store := newSessionStore(t)
	users, activity, h := newUserHandlerFixture()
	self := users.put(auth.User{Email: "self@example.com", Username: "self"})
	id := self.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/users/"+id+"/delete/",
		map[string]string{"id": id}, nil, &self, h.Delete)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 (delete must be rejected)", activity.count())
	}
	stillThere, _ := users.GetUserByIDAny(context.Background(), self.ID)
	if stillThere == nil {
		t.Fatal("self account should not have been deleted")
	}
}
