package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/session"
	"controlplane/web"
)

func TestOAuthLoginHandler_UnknownProviderReturns404(t *testing.T) {
	store := newSessionStore(t)
	flow := &fakeOAuthFlow{}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/login/"+id+"/",
		map[string]string{"id": id}, nil, nil, h.Login)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOAuthLoginHandler_RedirectsToAuthorizationURL(t *testing.T) {
	store := newSessionStore(t)
	provider := &authmodel.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{activeProvider: provider, authURL: "https://provider.example/authorize", state: "abc123"}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := provider.ID.String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/login/"+id+"/",
		map[string]string{"id": id}, nil, nil, h.Login)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != flow.authURL {
		t.Fatalf("Location = %q, want %q", got, flow.authURL)
	}
}

func TestOAuthCallbackHandler_UnknownProviderReturns404(t *testing.T) {
	store := newSessionStore(t)
	flow := &fakeOAuthFlow{}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/callback/"+id+"/",
		map[string]string{"id": id}, nil, nil, h.Callback)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOAuthCallbackHandler_ProviderErrorRedirectsToLogin(t *testing.T) {
	store := newSessionStore(t)
	provider := &authmodel.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{provider: provider}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := provider.ID.String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/callback/"+id+"/?error=access_denied",
		map[string]string{"id": id}, nil, nil, h.Callback)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/login/" {
		t.Fatalf("Location = %q, want /login/", got)
	}
}

func TestOAuthCallbackHandler_MissingCodeRedirectsToLogin(t *testing.T) {
	store := newSessionStore(t)
	provider := &authmodel.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{provider: provider}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := provider.ID.String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/callback/"+id+"/",
		map[string]string{"id": id}, nil, nil, h.Callback)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/login/" {
		t.Fatalf("Location = %q, want /login/", got)
	}
}

// TestOAuthCallbackHandler_ForcePasswordResetRedirectsToPasswordChange pins
// down that a freshly-provisioned OAuth user (Password: "!unusable",
// ForcePasswordReset: true) is routed through the same forced-reset flow
// admins use, rather than straight to the dashboard, so they set a real
// password before anything requiring one (e.g. revealing a secret) works.
func TestOAuthCallbackHandler_ForcePasswordResetRedirectsToPasswordChange(t *testing.T) {
	store := newSessionStore(t)
	provider := &authmodel.OAuthProvider{ID: uuid.New(), Name: "Google"}
	user := &authmodel.User{ID: uuid.New(), Email: "new@example.com", IsActive: true, ForcePasswordReset: true}
	flow := &fakeOAuthFlow{provider: provider, activeProvider: provider, token: &oauth2.Token{AccessToken: "tok"}, userInfo: map[string]any{}, user: user}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := provider.ID.String()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback/"+id+"/?code=abc&state=abc123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)

	handler := store.Middleware()(func(c echo.Context) error {
		session.FromContext(c).Set("oauth_state_"+id, "abc123")
		return h.Callback(c)
	})
	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/password/change/" {
		t.Fatalf("Location = %q, want /password/change/", got)
	}
}

func TestOAuthCallbackHandler_StateMismatchRedirectsToLogin(t *testing.T) {
	store := newSessionStore(t)
	provider := &authmodel.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{provider: provider}
	h := web.NewOAuthLoginHandler(flow, newFakeAuthStore(), &fakeActivityRecorder{}, "")

	id := provider.ID.String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/callback/"+id+"/?code=abc&state=unexpected",
		map[string]string{"id": id}, nil, nil, h.Callback)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/login/" {
		t.Fatalf("Location = %q, want /login/", got)
	}
}
