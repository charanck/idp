package e2e

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

var selfRegistrationDomainsRe = regexp.MustCompile(`(?s)id="self_registration_allowed_domains".*?value="([^"]*)"`)

// currentSelfRegistrationDomains scrapes the persisted
// self_registration_allowed_domains value off the rendered Policies page.
func (s *adminSession) currentSelfRegistrationDomains(t *testing.T) string {
	t.Helper()
	resp, err := s.http.Get(s.base + "/policies/")
	if err != nil {
		t.Fatalf("GET /policies/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /policies/: %v", err)
	}
	m := selfRegistrationDomainsRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("self_registration_allowed_domains field not found on /policies/")
	}
	return string(m[1])
}

// TestPoliciesShow_UpdatesSelfRegistrationAllowedDomains proves the
// singleton Policy's GET/POST round-trip actually persists, restoring the
// original value afterward since Policy is shared, live-instance state.
func TestPoliciesShow_UpdatesSelfRegistrationAllowedDomains(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	original := admin.currentSelfRegistrationDomains(t)
	t.Cleanup(func() {
		token := admin.csrfToken(t, "/policies/")
		resp, err := admin.http.PostForm(base+"/policies/", url.Values{
			"csrf_token":                        {token},
			"self_registration_allowed_domains": {original},
		})
		if err != nil {
			t.Logf("cleanup restore policy: %v", err)
			return
		}
		resp.Body.Close()
	})

	const updated = "example.com,example.org"
	token := admin.csrfToken(t, "/policies/")
	resp, err := admin.http.PostForm(base+"/policies/", url.Values{
		"csrf_token":                        {token},
		"self_registration_allowed_domains": {updated},
	})
	if err != nil {
		t.Fatalf("POST /policies/: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /policies/ status = %d, want 200", resp.StatusCode)
	}

	if got := admin.currentSelfRegistrationDomains(t); got != updated {
		t.Fatalf("self_registration_allowed_domains = %q, want %q", got, updated)
	}
}
