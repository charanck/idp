package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
)

// scopeServiceClientToApplications posts to /clients/:id/edit/ to restrict
// clientDBID's config/flag read scope to exactly applicationIDs.
func (s *adminSession) scopeServiceClientToApplications(t *testing.T, clientDBID string, applicationIDs []string) {
	t.Helper()
	token := s.csrfToken(t, "/clients/"+clientDBID+"/edit/")
	form := url.Values{"csrf_token": {token}}
	for _, appID := range applicationIDs {
		form.Add("application_ids", appID)
	}
	resp, err := s.http.PostForm(s.base+"/clients/"+clientDBID+"/edit/", form)
	if err != nil {
		t.Fatalf("scope service client: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("scope service client status = %d: %s", resp.StatusCode, body)
	}
}

// TestServiceClientApplicationScope_RestrictsConfigReadsToScopedApplication
// proves the Service Client Application allow-list (Part A/B S2S scoping)
// actually 404s config/flag reads for out-of-scope applications while
// allowing the scoped application's own reads to succeed.
func TestServiceClientApplicationScope_RestrictsConfigReadsToScopedApplication(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	allowedAppName, allowedAppID := admin.createApplication(t)
	allowedEnvName, allowedEnvID := admin.createEnvironment(t, allowedAppID)
	admin.createConfig(t, allowedAppID, allowedEnvID, "SCOPED_KEY", "scoped-value")

	otherAppName, otherAppID := admin.createApplication(t)
	otherEnvName, otherEnvID := admin.createEnvironment(t, otherAppID)
	admin.createConfig(t, otherAppID, otherEnvID, "OTHER_KEY", "other-value")

	apiKey, clientDBID := admin.createServiceClientWithID(t)
	admin.scopeServiceClientToApplications(t, clientDBID, []string{allowedAppID})

	allowedResp := apiRequest(t, base, http.MethodGet, "/api/v1/config/configs/list?service="+url.QueryEscape(allowedAppName)+"&environment="+url.QueryEscape(allowedEnvName), apiKey, nil)
	defer allowedResp.Body.Close()
	if allowedResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(allowedResp.Body)
		t.Fatalf("scoped application configs/list status = %d: %s", allowedResp.StatusCode, body)
	}

	otherResp := apiRequest(t, base, http.MethodGet, "/api/v1/config/configs/list?service="+url.QueryEscape(otherAppName)+"&environment="+url.QueryEscape(otherEnvName), apiKey, nil)
	defer otherResp.Body.Close()
	if otherResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(otherResp.Body)
		t.Fatalf("out-of-scope application configs/list status = %d, want 404: %s", otherResp.StatusCode, body)
	}

	allowedFlagsResp := apiRequest(t, base, http.MethodGet, "/api/v1/config/feature-flags?service="+url.QueryEscape(allowedAppName)+"&environment="+url.QueryEscape(allowedEnvName), apiKey, nil)
	defer allowedFlagsResp.Body.Close()
	if allowedFlagsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(allowedFlagsResp.Body)
		t.Fatalf("scoped application feature-flags status = %d: %s", allowedFlagsResp.StatusCode, body)
	}

	otherFlagsResp := apiRequest(t, base, http.MethodGet, "/api/v1/config/feature-flags?service="+url.QueryEscape(otherAppName)+"&environment="+url.QueryEscape(otherEnvName), apiKey, nil)
	defer otherFlagsResp.Body.Close()
	if otherFlagsResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(otherFlagsResp.Body)
		t.Fatalf("out-of-scope application feature-flags status = %d, want 404: %s", otherFlagsResp.StatusCode, body)
	}

	var payload []map[string]any
	if err := json.NewDecoder(allowedResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode scoped configs/list body: %v", err)
	}
}
