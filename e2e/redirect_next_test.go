package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// nextHiddenFieldRe extracts the login form's hidden "next" field, which
// echoes the post-login redirect target back through the POST so it survives
// a failed-login re-render (see web/auth_handler.go's loginData/Next).
var nextHiddenFieldRe = regexp.MustCompile(`name="next" value="([^"]*)"`)

// TestLoginPage_EchoesNextInFormAndOAuthLinks confirms /login/?next=... wires
// the requested redirect target into both the password form's hidden field
// and every "Continue with <provider>" link, so it survives whichever path
// the user actually takes through the page.
func TestLoginPage_EchoesNextInFormAndOAuthLinks(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	fake := fakeOAuthProviderServer(t, "unused@example.com")
	admin.createOAuthProvider(t, fake.URL)

	client := newAnonymousClient(t)
	next := "/profile/"
	resp, err := client.Get(base + "/login/?next=" + url.QueryEscape(next))
	if err != nil {
		t.Fatalf("GET /login/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	m := nextHiddenFieldRe.FindSubmatch(body)
	if m == nil || string(m[1]) != next {
		t.Fatalf("hidden next field = %v, want %q", m, next)
	}

	wantHrefFragment := "next=" + url.QueryEscape(next)
	if !strings.Contains(string(body), wantHrefFragment) {
		t.Fatalf("expected an oauth login link carrying %q, body:\n%s", wantHrefFragment, body)
	}
}

// TestLogin_RedirectsToSafeNextAfterPasswordLogin confirms a same-host path
// passed as next survives a successful password login instead of the usual
// postLoginLandingPath default.
func TestLogin_RedirectsToSafeNextAfterPasswordLogin(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email, password := admin.createUserReturningPassword(t)

	client := newAnonymousClient(t)
	token := csrfTokenFrom(t, client, base, "/login/")
	resp, err := client.PostForm(base+"/login/", url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {password},
		"next":       {"/profile/"},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/profile/" {
		t.Fatalf("Location = %q, want /profile/", loc)
	}

	profResp, err := client.Get(base + "/profile/")
	if err != nil {
		t.Fatalf("GET /profile/: %v", err)
	}
	defer profResp.Body.Close()
	if profResp.StatusCode != http.StatusOK {
		t.Fatalf("profile status = %d, want 200 (session should be authenticated)", profResp.StatusCode)
	}
}

// loginWithNext drives POST /login/ for email/password, optionally carrying
// a next value (form field, mirroring the hidden input the real login page
// renders), and returns the raw redirect response for the caller to inspect.
func loginWithNext(t *testing.T, client *http.Client, base, email, password, next string) *http.Response {
	t.Helper()
	token := csrfTokenFrom(t, client, base, "/login/")
	form := url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {password},
	}
	if next != "" {
		form.Set("next", next)
	}
	resp, err := client.PostForm(base+"/login/", form)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	return resp
}

// TestLogin_UnsafeAbsoluteNextFallsBackToDefaultLanding confirms a
// next=https://<other-host>/... value - the classic open-redirect payload -
// never reaches the Location header. Rather than hardcoding what
// postLoginLandingPath falls back to (which depends on the user's group
// permissions), this compares against a baseline login for the same user
// with no next at all - an unsafe next must behave identically to omitting
// it.
func TestLogin_UnsafeAbsoluteNextFallsBackToDefaultLanding(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email, password := admin.createUserReturningPassword(t)

	baseline := newAnonymousClient(t)
	baselineResp := loginWithNext(t, baseline, base, email, password, "")
	baselineResp.Body.Close()
	wantLocation := baselineResp.Header.Get("Location")

	client := newAnonymousClient(t)
	resp := loginWithNext(t, client, base, email, password, "https://evil.example/steal")
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != wantLocation {
		t.Fatalf("Location = %q, want %q (unsafe next must be ignored)", loc, wantLocation)
	}
}

// TestLogin_ProtocolRelativeNextFallsBackToDefaultLanding is like
// TestLogin_UnsafeAbsoluteNextFallsBackToDefaultLanding but for the
// protocol-relative "//evil.example/..." payload, which a naive
// strings.HasPrefix(next, "/") check would wrongly treat as a safe local
// path.
func TestLogin_ProtocolRelativeNextFallsBackToDefaultLanding(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email, password := admin.createUserReturningPassword(t)

	baseline := newAnonymousClient(t)
	baselineResp := loginWithNext(t, baseline, base, email, password, "")
	baselineResp.Body.Close()
	wantLocation := baselineResp.Header.Get("Location")

	client := newAnonymousClient(t)
	resp := loginWithNext(t, client, base, email, password, "//evil.example/steal")
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != wantLocation {
		t.Fatalf("Location = %q, want %q (protocol-relative next must be ignored)", loc, wantLocation)
	}
}

// loggedInNoRedirectClient logs client (already carrying no cookies) in as
// the bootstrap admin without following redirects, so callers can inspect
// the raw Location header - unlike adminSession.http (see
// TestLogout_DestroysSession), which follows redirects and would hide it.
func loggedInNoRedirectClient(t *testing.T) *http.Client {
	t.Helper()
	base := e2eBaseURL(t)
	client := newAnonymousClient(t)
	resp := loginWithNext(t, client, base, mustAdminEmail(t), mustAdminPassword(t), "")
	resp.Body.Close()
	return client
}

// TestLogin_AlreadyAuthenticatedRedirectsToSafeNext covers Login's early
// return for a caller who already has a session: GET /login/?next=... must
// bounce straight to next rather than re-rendering the form or ignoring the
// param.
func TestLogin_AlreadyAuthenticatedRedirectsToSafeNext(t *testing.T) {
	base := e2eBaseURL(t)
	client := loggedInNoRedirectClient(t)

	resp, err := client.Get(base + "/login/?next=" + url.QueryEscape("/profile/"))
	if err != nil {
		t.Fatalf("GET /login/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/profile/" {
		t.Fatalf("Location = %q, want /profile/", loc)
	}
}

// TestLogin_AlreadyAuthenticatedIgnoresUnsafeNext is the already-logged-in
// counterpart to TestLogin_UnsafeAbsoluteNextFallsBackToDefaultLanding:
// compares against a baseline GET /login/ (no next) for the same session
// rather than hardcoding the fallback path.
func TestLogin_AlreadyAuthenticatedIgnoresUnsafeNext(t *testing.T) {
	base := e2eBaseURL(t)
	client := loggedInNoRedirectClient(t)

	baselineResp, err := client.Get(base + "/login/")
	if err != nil {
		t.Fatalf("GET /login/ (baseline): %v", err)
	}
	baselineResp.Body.Close()
	if baselineResp.StatusCode != http.StatusFound {
		t.Fatalf("baseline status = %d, want 302", baselineResp.StatusCode)
	}
	wantLocation := baselineResp.Header.Get("Location")

	resp, err := client.Get(base + "/login/?next=" + url.QueryEscape("https://evil.example/steal"))
	if err != nil {
		t.Fatalf("GET /login/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != wantLocation {
		t.Fatalf("Location = %q, want %q (unsafe next must be ignored)", loc, wantLocation)
	}
}

// TestOAuthLogin_CallbackRedirectsToSafeNext mirrors
// TestLogin_RedirectsToSafeNextAfterPasswordLogin for the OAuth flow: next is
// captured off /oauth/login/:id/?next=... into the session (oauth_next_<id>)
// and popped back out on a successful /oauth/callback/:id/.
func TestOAuthLogin_CallbackRedirectsToSafeNext(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	userEmail := fmt.Sprintf("e2e-oauth-next-active-%d@example.com", time.Now().UnixNano())
	admin.createUser(t, userEmail, true)
	fake := fakeOAuthProviderServer(t, userEmail)
	providerID := admin.createOAuthProvider(t, fake.URL)

	client := newAnonymousClient(t)
	state := beginOAuthLoginWithNext(t, client, base, providerID, "/profile/")

	resp, err := client.Get(base + "/oauth/callback/" + providerID + "/?code=abc&state=" + state)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/profile/" {
		t.Fatalf("Location = %q, want /profile/", loc)
	}

	profResp, err := client.Get(base + "/profile/")
	if err != nil {
		t.Fatalf("GET /profile/: %v", err)
	}
	defer profResp.Body.Close()
	if profResp.StatusCode != http.StatusOK {
		t.Fatalf("profile status = %d, want 200 (session should be authenticated)", profResp.StatusCode)
	}
}

// TestOAuthLogin_CallbackIgnoresUnsafeNext confirms the same open-redirect
// guard applies on the OAuth path: an unsafe next is never even stored in
// the session (safeNext rejects it before sess.Set), so the callback falls
// back to the fixed /dashboard/ default.
func TestOAuthLogin_CallbackIgnoresUnsafeNext(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	userEmail := fmt.Sprintf("e2e-oauth-next-unsafe-%d@example.com", time.Now().UnixNano())
	admin.createUser(t, userEmail, true)
	fake := fakeOAuthProviderServer(t, userEmail)
	providerID := admin.createOAuthProvider(t, fake.URL)

	client := newAnonymousClient(t)
	state := beginOAuthLoginWithNext(t, client, base, providerID, "https://evil.example/steal")

	resp, err := client.Get(base + "/oauth/callback/" + providerID + "/?code=abc&state=" + state)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/ (unsafe next must be ignored)", loc)
	}
}
