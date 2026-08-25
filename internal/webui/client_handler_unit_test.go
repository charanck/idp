package webui_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/auth"
	"controlplane/internal/webui"
)

func newClientHandlerFixture() (*fakeClientStore, *fakeActivityRecorder, *webui.ClientHandler) {
	clients := newFakeClientStore()
	activity := &fakeActivityRecorder{}
	h := webui.NewClientHandler(clients, activity)
	return clients, activity, h
}

func TestClientListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	clients, _, h := newClientHandlerFixture()
	clients.put(auth.ServiceClient{Name: "svc-a", IsActive: true})

	rec := callHandler(t, store, http.MethodGet, "/clients/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestClientCreateHandler_DuplicateNameShowsError(t *testing.T) {
	store := newSessionStore(t)
	clients, activity, h := newClientHandlerFixture()
	clients.put(auth.ServiceClient{Name: "dup"})

	form := url.Values{"name": {"dup"}}
	rec := callHandler(t, store, http.MethodPost, "/clients/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (re-rendered form); body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 0 {
		t.Fatalf("activity.count() = %d, want 0 on failed create", activity.count())
	}
}

func TestClientCreateHandler_ShowsPlaintextAPIKeyOnce(t *testing.T) {
	store := newSessionStore(t)
	_, activity, h := newClientHandlerFixture()

	form := url.Values{"name": {"new-client"}}
	rec := callHandler(t, store, http.MethodPost, "/clients/create/", form, nil, h.Create)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestClientDetailHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newClientHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodGet, "/clients/"+unknown+"/",
		map[string]string{"id": unknown}, nil, nil, h.Detail)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestClientDetailHandler_MalformedIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newClientHandlerFixture()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/clients/not-a-uuid/",
		map[string]string{"id": "not-a-uuid"}, nil, nil, h.Detail)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestClientToggleHandler_TogglesActiveState(t *testing.T) {
	store := newSessionStore(t)
	clients, activity, h := newClientHandlerFixture()
	client := clients.put(auth.ServiceClient{Name: "svc-a", IsActive: true})
	id := client.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/clients/"+id+"/toggle/",
		map[string]string{"id": id}, nil, nil, h.Toggle)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestClientRegenerateKeyHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newClientHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodPost, "/clients/"+unknown+"/regenerate-key/",
		map[string]string{"id": unknown}, nil, nil, h.RegenerateKey)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
