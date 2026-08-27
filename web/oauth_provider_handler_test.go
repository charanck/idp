package web_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/auth"
	"controlplane/web"
)

func newOAuthProviderHandlerFixture() (*fakeOAuthProviderStore, *fakeActivityRecorder, *web.OAuthProviderHandler) {
	providers := newFakeOAuthProviderStore()
	activity := &fakeActivityRecorder{}
	h := web.NewOAuthProviderHandler(providers, activity)
	return providers, activity, h
}

func TestOAuthProviderListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	providers, _, h := newOAuthProviderHandlerFixture()
	providers.put(auth.OAuthProvider{Name: "Google", IsActive: true})

	rec := callHandler(t, store, http.MethodGet, "/oauth/providers/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestOAuthProviderCreateHandler_MissingRequiredFieldsShowsError(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newOAuthProviderHandlerFixture()

	form := url.Values{"name": {"Google"}}
	rec := callHandler(t, store, http.MethodPost, "/oauth/providers/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

func TestOAuthProviderCreateHandler_PersistsAndRedirects(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newOAuthProviderHandlerFixture()

	form := url.Values{
		"name": {"Google"}, "client_id": {"cid"}, "client_secret": {"secret"},
		"authorization_url": {"https://accounts.google.com/o/oauth2/auth"},
		"token_url":         {"https://oauth2.googleapis.com/token"},
	}
	rec := callHandler(t, store, http.MethodPost, "/oauth/providers/create/", form, nil, h.Create)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestOAuthProviderEditHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newOAuthProviderHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/providers/"+unknown+"/edit/",
		map[string]string{"id": unknown}, nil, nil, h.Edit)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOAuthProviderToggleHandler_TogglesActiveState(t *testing.T) {
	store := newSessionStore(t)
	providers, activity, h := newOAuthProviderHandlerFixture()
	p := providers.put(auth.OAuthProvider{Name: "Google", IsActive: false})
	id := p.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/oauth/providers/"+id+"/toggle/",
		map[string]string{"id": id}, nil, nil, h.Toggle)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestOAuthProviderDeleteHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newOAuthProviderHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/providers/"+unknown+"/delete/",
		map[string]string{"id": unknown}, nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
