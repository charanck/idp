package web_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	authmodel "controlplane/internal/model/auth"
	"controlplane/web"
)

func newClientHandlerFixture() (*fakeClientStore, *fakeActivityRecorder, *web.ClientHandler) {
	clients := newFakeClientStore()
	activity := &fakeActivityRecorder{}
	h := web.NewClientHandler(clients, activity)
	return clients, activity, h
}

func TestClientListHandler_ReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	clients, _, h := newClientHandlerFixture()
	clients.put(authmodel.ServiceClient{Name: "svc-a", IsActive: true})

	rec := callHandler(t, store, http.MethodGet, "/clients/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestClientCreateHandler_DuplicateNameShowsError(t *testing.T) {
	store := newSessionStore(t)
	clients, activity, h := newClientHandlerFixture()
	clients.put(authmodel.ServiceClient{Name: "dup"})

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
	client := clients.put(authmodel.ServiceClient{Name: "svc-a", IsActive: true})
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

func TestClientDeleteHandler_GetShowsConfirmationWithoutDeleting(t *testing.T) {
	store := newSessionStore(t)
	clients, _, h := newClientHandlerFixture()
	client := clients.put(authmodel.ServiceClient{Name: "svc-a"})
	id := client.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/clients/"+id+"/delete/",
		map[string]string{"id": id}, nil, nil, h.Delete)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, _ := clients.GetServiceClientByIDAny(context.Background(), client.ID); got == nil {
		t.Fatal("expected GET not to delete the client")
	}
}

func TestClientDeleteHandler_PostDeletesClient(t *testing.T) {
	store := newSessionStore(t)
	clients, activity, h := newClientHandlerFixture()
	client := clients.put(authmodel.ServiceClient{Name: "svc-a"})
	id := client.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/clients/"+id+"/delete/",
		map[string]string{"id": id}, nil, nil, h.Delete)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got, _ := clients.GetServiceClientByIDAny(context.Background(), client.ID); got != nil {
		t.Fatal("expected the client to be deleted")
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}

func TestClientDeleteHandler_UnknownIDReturns404(t *testing.T) {
	store := newSessionStore(t)
	_, _, h := newClientHandlerFixture()

	unknown := uuid.New().String()
	rec := callHandlerWithParams(t, store, http.MethodPost, "/clients/"+unknown+"/delete/",
		map[string]string{"id": unknown}, nil, nil, h.Delete)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestClientRegenerateKeyHandler_GetDoesNotChangeTheKey(t *testing.T) {
	store := newSessionStore(t)
	clients, _, h := newClientHandlerFixture()
	client := clients.put(authmodel.ServiceClient{Name: "svc-a", EncryptionKey: "original-key"})
	id := client.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodGet, "/clients/"+id+"/regenerate-key/",
		map[string]string{"id": id}, nil, nil, h.RegenerateKey)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	current, _ := clients.GetServiceClientByIDAny(context.Background(), client.ID)
	if current == nil || current.EncryptionKey != "original-key" {
		t.Fatalf("expected GET not to change the encryption key, got %+v", current)
	}
}

func TestClientRegenerateKeyHandler_PostIssuesANewKey(t *testing.T) {
	store := newSessionStore(t)
	clients, activity, h := newClientHandlerFixture()
	client := clients.put(authmodel.ServiceClient{Name: "svc-a", EncryptionKey: "original-key"})
	id := client.ID.String()

	rec := callHandlerWithParams(t, store, http.MethodPost, "/clients/"+id+"/regenerate-key/",
		map[string]string{"id": id}, nil, nil, h.RegenerateKey)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	current, _ := clients.GetServiceClientByIDAny(context.Background(), client.ID)
	if current == nil || current.EncryptionKey == "original-key" || current.EncryptionKey == "" {
		t.Fatalf("expected a new, non-empty encryption key, got %+v", current)
	}
	if activity.count() != 1 {
		t.Fatalf("activity.count() = %d, want 1", activity.count())
	}
}
