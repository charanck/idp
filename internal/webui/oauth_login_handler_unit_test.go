package webui_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/auth"
	"controlplane/internal/webui"
)

func TestOAuthLoginHandler_UnknownProviderReturns404(t *testing.T) {
	store := newSessionStore(t)
	flow := &fakeOAuthFlow{}
	h := webui.NewOAuthLoginHandler(flow, &fakeActivityRecorder{})

	id := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/login/"+id+"/",
		map[string]string{"id": id}, nil, nil, h.Login)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOAuthLoginHandler_RedirectsToAuthorizationURL(t *testing.T) {
	store := newSessionStore(t)
	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{activeProvider: provider, authURL: "https://provider.example/authorize", state: "abc123"}
	h := webui.NewOAuthLoginHandler(flow, &fakeActivityRecorder{})

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
	h := webui.NewOAuthLoginHandler(flow, &fakeActivityRecorder{})

	id := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/oauth/callback/"+id+"/",
		map[string]string{"id": id}, nil, nil, h.Callback)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOAuthCallbackHandler_ProviderErrorRedirectsToLogin(t *testing.T) {
	store := newSessionStore(t)
	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{provider: provider}
	h := webui.NewOAuthLoginHandler(flow, &fakeActivityRecorder{})

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
	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{provider: provider}
	h := webui.NewOAuthLoginHandler(flow, &fakeActivityRecorder{})

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

func TestOAuthCallbackHandler_StateMismatchRedirectsToLogin(t *testing.T) {
	store := newSessionStore(t)
	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "Google"}
	flow := &fakeOAuthFlow{provider: provider}
	h := webui.NewOAuthLoginHandler(flow, &fakeActivityRecorder{})

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
