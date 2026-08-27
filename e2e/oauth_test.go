package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"
)

var oauthProviderEditIDRe = regexp.MustCompile(`/oauth/providers/([0-9a-fA-F-]{36})/edit/`)

// fakeOAuthProviderServer stands in for a real IdP's token + userinfo
// endpoints. It's bound to localhost and reachable by the live
// control-plane instance under test as long as that instance runs on the
// same host (e.g. `go run ./cmd/server` alongside this test binary).
func fakeOAuthProviderServer(t *testing.T, email string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer"})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"sub": fmt.Sprintf("%d", time.Now().UnixNano()), "email": email})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// createOAuthProvider creates a uniquely-named, active, auto-create-enabled
// OAuth provider pointed at providerURL via the admin web UI, returning its
// ID.
func (s *adminSession) createOAuthProvider(t *testing.T, providerURL string) string {
	t.Helper()
	name := fmt.Sprintf("e2e-provider-%d", time.Now().UnixNano())
	token := s.csrfToken(t, "/oauth/providers/create/")
	resp, err := s.http.PostForm(s.base+"/oauth/providers/create/", url.Values{
		"csrf_token":         {token},
		"name":               {name},
		"client_id":          {"client-id"},
		"client_secret":      {"client-secret"},
		"authorization_url":  {providerURL + "/authorize"},
		"token_url":          {providerURL + "/token"},
		"userinfo_url":       {providerURL + "/userinfo"},
		"auto_create_users":  {"on"},
		"is_active":          {"on"},
	})
	if err != nil {
		t.Fatalf("create oauth provider: %v", err)
	}
	resp.Body.Close()

	listResp, err := s.http.Get(s.base + "/oauth/providers/?q=" + url.QueryEscape(name))
	if err != nil {
		t.Fatalf("list oauth providers: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read oauth providers list: %v", err)
	}
	m := oauthProviderEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no oauth provider ID found for %q", name)
	}
	return string(m[1])
}

// newAnonymousClient returns a cookie-carrying client (for session
// persistence across the login -> callback -> dashboard flow) that does not
// follow redirects, so tests can assert on the redirect response itself.
func newAnonymousClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// beginOAuthLogin drives the real /oauth/login/:id/ endpoint and pulls the
// state back out of the resulting redirect Location's query string, mirroring
// what a real browser would carry through to the callback.
func beginOAuthLogin(t *testing.T, client *http.Client, base, providerID string) string {
	t.Helper()
	resp, err := client.Get(base + "/oauth/login/" + providerID + "/")
	if err != nil {
		t.Fatalf("GET /oauth/login/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("oauth login status = %d, want 302", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("resp.Location: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatalf("no state in authorization URL: %s", loc)
	}
	return state
}

// assertNoAuthenticatedSession confirms the session cookie held by client
// does not grant access to an authenticated-only page.
func assertNoAuthenticatedSession(t *testing.T, client *http.Client, base string) {
	t.Helper()
	resp, err := client.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("dashboard status = %d, want 302 (redirect to login, no session)", resp.StatusCode)
	}
}

// createUser creates a user via the admin web UI with a random password,
// used to seed a pre-existing account for the OAuth flow to link against.
func (s *adminSession) createUser(t *testing.T, email string, isActive bool) {
	t.Helper()
	token := s.csrfToken(t, "/users/create/")
	form := url.Values{
		"csrf_token": {token},
		"email":      {email},
		"username":   {fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())},
		"password1":  {"password12345"},
		"password2":  {"password12345"},
	}
	if isActive {
		form.Set("is_active", "on")
	}
	resp, err := s.http.PostForm(s.base+"/users/create/", form)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	resp.Body.Close()
}

func TestOAuthCallback_RejectsLoginForExistingButInactiveUser(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email := fmt.Sprintf("e2e-oauth-%d@example.com", time.Now().UnixNano())
	admin.createUser(t, email, false)
	fake := fakeOAuthProviderServer(t, email)
	providerID := admin.createOAuthProvider(t, fake.URL)

	client := newAnonymousClient(t)
	state := beginOAuthLogin(t, client, base, providerID)

	resp, err := client.Get(base + "/oauth/callback/" + providerID + "/?code=abc&state=" + state)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login/" {
		t.Fatalf("Location = %q, want /login/", loc)
	}
	assertNoAuthenticatedSession(t, client, base)
}

func TestOAuthCallback_AllowsLoginForActivatedUser(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email := fmt.Sprintf("e2e-oauth-%d@example.com", time.Now().UnixNano())
	admin.createUser(t, email, true)
	fake := fakeOAuthProviderServer(t, email)
	providerID := admin.createOAuthProvider(t, fake.URL)

	client := newAnonymousClient(t)
	state := beginOAuthLogin(t, client, base, providerID)

	resp, err := client.Get(base + "/oauth/callback/" + providerID + "/?code=abc&state=" + state)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/", loc)
	}

	dashResp, err := client.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer dashResp.Body.Close()
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200 (session should be authenticated)", dashResp.StatusCode)
	}
}

func TestOAuthCallback_RejectsLoginForNewlyAutoCreatedUser(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email := fmt.Sprintf("e2e-oauth-%d@example.com", time.Now().UnixNano())
	fake := fakeOAuthProviderServer(t, email)
	providerID := admin.createOAuthProvider(t, fake.URL)

	client := newAnonymousClient(t)
	state := beginOAuthLogin(t, client, base, providerID)

	resp, err := client.Get(base + "/oauth/callback/" + providerID + "/?code=abc&state=" + state)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login/" {
		t.Fatalf("Location = %q, want /login/", loc)
	}
	assertNoAuthenticatedSession(t, client, base)
}
