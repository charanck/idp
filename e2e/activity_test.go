package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestActivityLog_RecordsApplicationCreation proves the append-only audit
// log actually gets written to by a real web UI action, not just that its
// own page renders.
func TestActivityLog_RecordsApplicationCreation(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	name, _ := admin.createApplication(t)

	resp, err := admin.http.Get(base + "/activity/?resource=application")
	if err != nil {
		t.Fatalf("GET /activity/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	if !strings.Contains(string(body), name) {
		t.Fatalf("expected activity log to mention newly created application %q: %s", name, body)
	}
}

func TestActivityLog_RequiresAdmin(t *testing.T) {
	base := e2eBaseURL(t)
	anon := newAnonymousClient(t)

	resp, err := anon.Get(base + "/activity/")
	if err != nil {
		t.Fatalf("GET /activity/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}

func TestDashboard_RendersForLoggedInUser(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	resp, err := admin.http.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
