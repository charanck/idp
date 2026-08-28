package e2e

import (
	"io"
	"net/http"
	"net/url"
	"testing"
)

// TestOAuthProvidersCRUD_EditToggleDelete covers the OAuth provider admin
// pages not already exercised by oauth_test.go's login-flow tests (which
// only create a provider, never edit/toggle/delete one).
func TestOAuthProvidersCRUD_EditToggleDelete(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	fake := fakeOAuthProviderServer(t, "unused@example.com")
	id := admin.createOAuthProvider(t, fake.URL)

	editToken := admin.csrfToken(t, "/oauth/providers/"+id+"/edit/")
	editResp, err := admin.http.PostForm(base+"/oauth/providers/"+id+"/edit/", url.Values{
		"csrf_token":        {editToken},
		"name":              {"e2e-provider-renamed"},
		"client_id":         {"client-id"},
		"client_secret":     {"client-secret"},
		"authorization_url": {fake.URL + "/authorize"},
		"token_url":         {fake.URL + "/token"},
		"userinfo_url":      {fake.URL + "/userinfo"},
		"auto_create_users": {"on"},
		"is_active":         {"on"},
	})
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	editResp.Body.Close()

	listResp, err := admin.http.Get(base + "/oauth/providers/?q=e2e-provider-renamed")
	if err != nil {
		t.Fatalf("GET oauth providers list: %v", err)
	}
	listBody, err := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if err != nil {
		t.Fatalf("read oauth providers list: %v", err)
	}
	if !oauthProviderEditIDRe.Match(listBody) {
		t.Fatalf("renamed provider not found in list: %s", listBody)
	}

	toggleToken := admin.csrfToken(t, "/oauth/providers/?q=e2e-provider-renamed")
	toggleResp, err := admin.http.PostForm(base+"/oauth/providers/"+id+"/toggle/", url.Values{
		"csrf_token": {toggleToken},
	})
	if err != nil {
		t.Fatalf("POST toggle: %v", err)
	}
	toggleResp.Body.Close()
	if toggleResp.StatusCode != http.StatusOK && toggleResp.StatusCode != http.StatusFound {
		t.Fatalf("toggle status = %d, want 200 or 302", toggleResp.StatusCode)
	}

	deleteToken := admin.csrfToken(t, "/oauth/providers/"+id+"/delete/")
	deleteResp, err := admin.http.PostForm(base+"/oauth/providers/"+id+"/delete/", url.Values{
		"csrf_token": {deleteToken},
	})
	if err != nil {
		t.Fatalf("POST delete: %v", err)
	}
	deleteResp.Body.Close()

	afterDelete, err := admin.http.Get(base + "/oauth/providers/?q=e2e-provider-renamed")
	if err != nil {
		t.Fatalf("GET oauth providers list after delete: %v", err)
	}
	defer afterDelete.Body.Close()
	afterDeleteBody, err := io.ReadAll(afterDelete.Body)
	if err != nil {
		t.Fatalf("read oauth providers list after delete: %v", err)
	}
	if oauthProviderEditIDRe.Match(afterDeleteBody) {
		t.Fatalf("deleted provider still present in list: %s", afterDeleteBody)
	}
}
