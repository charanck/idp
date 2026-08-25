package webui_test

import (
	"net/http"
	"testing"

	"controlplane/internal/config"
	"controlplane/internal/webui"
)

func TestActivityListHandler_FiltersByResourceAndType(t *testing.T) {
	store := newSessionStore(t)
	reader := &fakeActivityReader{
		rows: []config.Activity{
			{Type: "create", Resource: "application"},
			{Type: "update", Resource: "application"},
			{Type: "create", Resource: "config"},
		},
		resources: []string{"application", "config"},
		types:     []string{"create", "update"},
	}
	h := webui.NewActivityHandler(reader)

	rec := callHandler(t, store, http.MethodGet, "/activity/?resource=application&type=create", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestActivityListHandler_NoFiltersReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	reader := &fakeActivityReader{}
	h := webui.NewActivityHandler(reader)

	rec := callHandler(t, store, http.MethodGet, "/activity/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
