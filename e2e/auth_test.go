package e2e

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestLogin_SucceedsWithValidCredentials uses a freshly created member user
// rather than the bootstrap admin: admin.go unconditionally sets
// ForcePasswordReset=true on the admin account on every server startup, so
// asserting a /dashboard/ redirect for the admin would be flaky depending on
// whether this process already drove it through /password/change/.
func TestLogin_SucceedsWithValidCredentials(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email, password := admin.createUserReturningPassword(t)

	client := newAnonymousClient(t)
	token := csrfTokenFrom(t, client, base, "/login/")
	resp, err := client.PostForm(base+"/login/", url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {password},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
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
		t.Fatalf("dashboard status = %d, want 200", dashResp.StatusCode)
	}
}

func TestLogin_RejectsWrongPassword(t *testing.T) {
	base := e2eBaseURL(t)
	email := mustAdminEmail(t)

	client := newAnonymousClient(t)
	token := csrfTokenFrom(t, client, base, "/login/")
	resp, err := client.PostForm(base+"/login/", url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {"definitely-the-wrong-password"},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered login form)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Invalid email or password") {
		t.Fatalf("expected login error message in body: %s", body)
	}

	assertNoAuthenticatedSession(t, client, base)
}

func TestRegister_AlwaysDisabled(t *testing.T) {
	base := e2eBaseURL(t)
	client := newAnonymousClient(t)

	// GET /register/ never renders a registration form (it redirects
	// unconditionally, same as POST), so the CSRF token has to come from
	// another page that shares the same session - it's session-scoped, not
	// page-scoped.
	getResp, err := client.Get(base + "/register/")
	if err != nil {
		t.Fatalf("GET /register/: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusFound {
		t.Fatalf("GET /register/ status = %d, want 302", getResp.StatusCode)
	}
	if loc := getResp.Header.Get("Location"); loc != "/login/" {
		t.Fatalf("GET /register/ Location = %q, want /login/", loc)
	}

	token := csrfTokenFrom(t, client, base, "/login/")

	resp, err := client.PostForm(base+"/register/", url.Values{
		"csrf_token": {token},
		"email":      {"nobody@example.com"},
		"username":   {"nobody"},
		"password1":  {"password12345"},
		"password2":  {"password12345"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
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

func TestLogout_DestroysSession(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	dashResp, err := admin.http.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/ before logout: %v", err)
	}
	dashResp.Body.Close()
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status before logout = %d, want 200", dashResp.StatusCode)
	}

	token := admin.csrfToken(t, "/dashboard/")
	logoutResp, err := admin.http.PostForm(base+"/logout/", url.Values{
		"csrf_token": {token},
	})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	logoutResp.Body.Close()

	// admin.http follows redirects (unlike newAnonymousClient), so
	// assertNoAuthenticatedSession's raw-302 check doesn't apply here -
	// instead confirm the followed chain lands on the login page.
	dash2Resp, err := admin.http.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/ after logout: %v", err)
	}
	defer dash2Resp.Body.Close()
	if got := dash2Resp.Request.URL.Path; got != "/login/" {
		t.Fatalf("after logout, GET /dashboard/ ended up at %q, want /login/", got)
	}
}

// TestPasswordChange_UpdatesCredentialsAndOldPasswordStopsWorking drives a
// freshly created user through the password-change form, then confirms both
// that the new password works and the old one no longer does.
func TestPasswordChange_UpdatesCredentialsAndOldPasswordStopsWorking(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	email, initialPassword := admin.createUserReturningPassword(t)

	member := newAnonymousClient(t)
	loginToken := csrfTokenFrom(t, member, base, "/login/")
	loginResp, err := member.PostForm(base+"/login/", url.Values{
		"csrf_token": {loginToken},
		"username":   {email},
		"password":   {initialPassword},
	})
	if err != nil {
		t.Fatalf("initial login: %v", err)
	}
	loginResp.Body.Close()

	newPassword := "a-brand-new-password-1"
	changeToken := csrfTokenFrom(t, member, base, "/password/change/")
	changeResp, err := member.PostForm(base+"/password/change/", url.Values{
		"csrf_token":    {changeToken},
		"old_password":  {initialPassword},
		"new_password1": {newPassword},
		"new_password2": {newPassword},
	})
	if err != nil {
		t.Fatalf("password change: %v", err)
	}
	changeResp.Body.Close()
	if changeResp.StatusCode != http.StatusFound {
		t.Fatalf("password change status = %d, want 302", changeResp.StatusCode)
	}

	// Old password must no longer work.
	stale := newAnonymousClient(t)
	staleToken := csrfTokenFrom(t, stale, base, "/login/")
	staleResp, err := stale.PostForm(base+"/login/", url.Values{
		"csrf_token": {staleToken},
		"username":   {email},
		"password":   {initialPassword},
	})
	if err != nil {
		t.Fatalf("login with old password: %v", err)
	}
	defer staleResp.Body.Close()
	assertNoAuthenticatedSession(t, stale, base)

	// New password must work.
	fresh := newAnonymousClient(t)
	freshToken := csrfTokenFrom(t, fresh, base, "/login/")
	freshResp, err := fresh.PostForm(base+"/login/", url.Values{
		"csrf_token": {freshToken},
		"username":   {email},
		"password":   {newPassword},
	})
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	defer freshResp.Body.Close()
	if freshResp.StatusCode != http.StatusFound || freshResp.Header.Get("Location") != "/dashboard/" {
		t.Fatalf("login with new password: status = %d, Location = %q", freshResp.StatusCode, freshResp.Header.Get("Location"))
	}
}
