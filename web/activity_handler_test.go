package web_test

import (
	"net/http"
	"testing"

	activitymodel "controlplane/internal/model/activity"
	"controlplane/web"
)

func TestActivityListHandler_FiltersByResourceAndType(t *testing.T) {
	store := newSessionStore(t)
	reader := &fakeActivityReader{
		rows: []activitymodel.Activity{
			{Type: "create", Resource: "application"},
			{Type: "update", Resource: "application"},
			{Type: "create", Resource: "config"},
		},
		resources: []string{"application", "config"},
		types:     []string{"create", "update"},
	}
	h := web.NewActivityHandler(reader)

	rec := callHandler(t, store, http.MethodGet, "/activity/?resource=application&type=create", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestActivityListHandler_NoFiltersReturnsOK(t *testing.T) {
	store := newSessionStore(t)
	reader := &fakeActivityReader{}
	h := web.NewActivityHandler(reader)

	rec := callHandler(t, store, http.MethodGet, "/activity/", nil, nil, h.List)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
