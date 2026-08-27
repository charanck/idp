package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"
)

var clientIDInBodyRe = regexp.MustCompile(`/clients/([0-9a-fA-F-]{36})/`)

// createServiceClientWithID creates a uniquely-named service client via the
// web UI, returning both its X-API-Key and its ID, parsed off the
// create-success page's "View Details" link.
func (s *adminSession) createServiceClientWithID(t *testing.T) (apiKey, id string) {
	t.Helper()
	name := fmt.Sprintf("e2e-client-%d", time.Now().UnixNano())
	token := s.csrfToken(t, "/clients/create/")
	resp, err := s.http.PostForm(s.base+"/clients/create/", url.Values{
		"csrf_token": {token},
		"name":       {name},
	})
	if err != nil {
		t.Fatalf("create service client: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read create-client response: %v", err)
	}
	keyM := apiKeyRe.FindSubmatch(body)
	if keyM == nil {
		t.Fatalf("no API key found in create-client response")
	}
	idM := clientIDInBodyRe.FindSubmatch(body)
	if idM == nil {
		t.Fatalf("no client ID found in create-client response")
	}
	return string(keyM[1]), string(idM[1])
}

// TestClientDelete_RequiresAdmin exercises the real admin-only middleware
// group: an unauthenticated request must be bounced to /login/ rather than
// deleting the client, which only live routing/middleware wiring can prove.
func TestClientDelete_RequiresAdmin(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	_, id := admin.createServiceClientWithID(t)

	anon := newAnonymousClient(t)
	token := csrfTokenFrom(t, anon, base, "/login/")
	resp, err := anon.PostForm(base+"/clients/"+id+"/delete/", url.Values{
		"csrf_token": {token},
	})
	if err != nil {
		t.Fatalf("POST delete as anonymous: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}

	detail, err := admin.http.Get(base + "/clients/" + id + "/")
	if err != nil {
		t.Fatalf("GET client detail as admin: %v", err)
	}
	defer detail.Body.Close()
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("expected the client to survive an unauthenticated delete attempt, status = %d", detail.StatusCode)
	}
}

// TestClientRegenerateKey_RequiresAdmin mirrors TestClientDelete_RequiresAdmin
// for the regenerate-key route.
func TestClientRegenerateKey_RequiresAdmin(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	_, id := admin.createServiceClientWithID(t)

	anon := newAnonymousClient(t)
	token := csrfTokenFrom(t, anon, base, "/login/")
	resp, err := anon.PostForm(base+"/clients/"+id+"/regenerate-key/", url.Values{
		"csrf_token": {token},
	})
	if err != nil {
		t.Fatalf("POST regenerate-key as anonymous: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}

// csrfTokenFrom is like adminSession.csrfToken but for any cookie-carrying
// client, not just a logged-in admin one.
func csrfTokenFrom(t *testing.T, client *http.Client, base, path string) string {
	t.Helper()
	resp, err := client.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := csrfTokenRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no csrf_token found on %s", path)
	}
	return string(m[1])
}
