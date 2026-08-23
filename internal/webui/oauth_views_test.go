// Integration test for the OAuth login callback in oauth_login_handlers.go -
// specifically that a session is never established for a not-yet-activated
// user, mirroring web_ui/tests/test_oauth_views.py's TestOAuthCallbackActivation.
package webui_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"controlplane/internal/auth"
)

// fakeOAuthProviderServer stands in for the real IdP's token + userinfo
// endpoints (Django's tests mock oauth_service.exchange_code_for_token/
// get_user_info directly; the Go port drives the same outcome through real
// HTTP calls, since OAuthService always talks to the provider over the
// network rather than through an injectable interface).
func fakeOAuthProviderServer(t *testing.T, email string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok", "token_type": "Bearer",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sub": "1", "email": email,
		})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestOAuthCallback_RejectsLoginForNewlyAutoCreatedUser(t *testing.T) {
	gdb, e, _ := setupWebUI(t)
	fake := fakeOAuthProviderServer(t, "newperson@example.com")
	baseURL := newTestServer(t, e)

	userinfoURL := fake.URL + "/userinfo"
	provider := &auth.OAuthProvider{
		Name: "Google", ClientID: "client-id", ClientSecret: "client-secret",
		AuthorizationURL: fake.URL + "/authorize", TokenURL: fake.URL + "/token",
		UserinfoURL: &userinfoURL, IsActive: true, AutoCreateUsers: true,
	}
	if err := gdb.Create(provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}

	client := newTestClient(t)
	state := seedOAuthState(t, client, baseURL, provider.ID.String())

	resp, err := client.Get(baseURL + "/oauth/callback/" + provider.ID.String() + "/?code=abc&state=" + state)
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

	var user auth.User
	if err := gdb.Where("email = ?", "newperson@example.com").First(&user).Error; err != nil {
		t.Fatalf("expected the user to have been auto-created: %v", err)
	}
	if user.IsActive {
		t.Fatal("expected the newly auto-created user to be inactive")
	}
	assertNoAuthenticatedSession(t, client, baseURL)
}

func TestOAuthCallback_RejectsLoginForExistingButInactiveUser(t *testing.T) {
	gdb, e, _ := setupWebUI(t)
	fake := fakeOAuthProviderServer(t, "newperson@example.com")
	baseURL := newTestServer(t, e)

	if err := gdb.Create(&auth.User{Email: "newperson@example.com", Username: "newperson", IsActive: false}).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	userinfoURL := fake.URL + "/userinfo"
	provider := &auth.OAuthProvider{
		Name: "Google", ClientID: "client-id", ClientSecret: "client-secret",
		AuthorizationURL: fake.URL + "/authorize", TokenURL: fake.URL + "/token",
		UserinfoURL: &userinfoURL, IsActive: true, AutoCreateUsers: true,
	}
	if err := gdb.Create(provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}

	client := newTestClient(t)
	state := seedOAuthState(t, client, baseURL, provider.ID.String())

	resp, err := client.Get(baseURL + "/oauth/callback/" + provider.ID.String() + "/?code=abc&state=" + state)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	assertNoAuthenticatedSession(t, client, baseURL)
}

func TestOAuthCallback_AllowsLoginForActivatedUser(t *testing.T) {
	gdb, e, _ := setupWebUI(t)
	fake := fakeOAuthProviderServer(t, "newperson@example.com")
	baseURL := newTestServer(t, e)

	if err := gdb.Create(&auth.User{Email: "newperson@example.com", Username: "newperson", IsActive: true}).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	userinfoURL := fake.URL + "/userinfo"
	provider := &auth.OAuthProvider{
		Name: "Google", ClientID: "client-id", ClientSecret: "client-secret",
		AuthorizationURL: fake.URL + "/authorize", TokenURL: fake.URL + "/token",
		UserinfoURL: &userinfoURL, IsActive: true, AutoCreateUsers: true,
	}
	if err := gdb.Create(provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}

	client := newTestClient(t)
	state := seedOAuthState(t, client, baseURL, provider.ID.String())

	resp, err := client.Get(baseURL + "/oauth/callback/" + provider.ID.String() + "/?code=abc&state=" + state)
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

	// A logged-in session must now let us reach an authenticated page
	// without being bounced to /login/.
	dashResp, err := client.Get(baseURL + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer dashResp.Body.Close()
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200 (session should be authenticated)", dashResp.StatusCode)
	}
}

// seedOAuthState mimics oauth_login_handlers.go's oauthLoginHandler storing
// "expected-state" in the session before redirecting to the provider, by
// driving the real /oauth/login/:id/ endpoint and pulling the state back out
// of the resulting redirect Location's query string.
func seedOAuthState(t *testing.T, client *http.Client, baseURL, providerID string) string {
	t.Helper()
	resp, err := client.Get(baseURL + "/oauth/login/" + providerID + "/")
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
func assertNoAuthenticatedSession(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	resp, err := client.Get(baseURL + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("dashboard status = %d, want 302 (redirect to login, no session)", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login/?next=%2Fdashboard%2F" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}
