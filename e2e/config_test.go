package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"
)

var applicationEditIDRe = regexp.MustCompile(`/applications/([0-9a-fA-F-]{36})/edit/`)
var environmentEditIDRe = regexp.MustCompile(`/environments/([0-9a-fA-F-]{36})/edit/`)

// createApplication creates a uniquely-named application via the web UI and
// returns its name and ID.
func (s *adminSession) createApplication(t *testing.T) (name, id string) {
	t.Helper()
	name = fmt.Sprintf("e2e-app-%d", time.Now().UnixNano())
	token := s.csrfToken(t, "/applications/create/")
	resp, err := s.http.PostForm(s.base+"/applications/create/", url.Values{
		"csrf_token": {token},
		"name":       {name},
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	resp.Body.Close()

	listResp, err := s.http.Get(s.base + "/applications/?q=" + url.QueryEscape(name))
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read applications list: %v", err)
	}
	m := applicationEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no application ID found for %q", name)
	}
	return name, string(m[1])
}

// createEnvironment creates a uniquely-named environment under applicationID
// via the web UI and returns its name and ID.
func (s *adminSession) createEnvironment(t *testing.T, applicationID string) (name, id string) {
	t.Helper()
	name = fmt.Sprintf("e2e-env-%d", time.Now().UnixNano())
	token := s.csrfToken(t, "/environments/create/")
	resp, err := s.http.PostForm(s.base+"/environments/create/", url.Values{
		"csrf_token":     {token},
		"application_id": {applicationID},
		"name":           {name},
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	resp.Body.Close()

	listResp, err := s.http.Get(s.base + "/environments/?application_id=" + url.QueryEscape(applicationID) + "&q=" + url.QueryEscape(name))
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read environments list: %v", err)
	}
	m := environmentEditIDRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no environment ID found for %q", name)
	}
	return name, string(m[1])
}

// createConfig creates a config entry under applicationID/environmentID via
// the web UI.
func (s *adminSession) createConfig(t *testing.T, applicationID, environmentID, key, value string) {
	t.Helper()
	token := s.csrfToken(t, "/configs/create/")
	resp, err := s.http.PostForm(s.base+"/configs/create/", url.Values{
		"csrf_token":     {token},
		"application_id": {applicationID},
		"environment_id": {environmentID},
		"key":            {key},
		"value":          {value},
		"type":           {"string"},
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		t.Fatalf("create config status = %d, want 200 or 302", resp.StatusCode)
	}
}

func TestListConfigsForClientEndpoint_ReturnsConfigsEncryptedWithClientKey(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	apiKey := admin.createServiceClient(t)
	appName, appID := admin.createApplication(t)
	envName, envID := admin.createEnvironment(t, appID)
	admin.createConfig(t, appID, envID, "API_URL", "https://api.example.com")

	resp := apiRequest(t, base, http.MethodGet, "/api/v1/config/configs/list?service="+appName+"&environment="+envName, apiKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 1 || body[0].Key != "API_URL" {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body[0].Value == "https://api.example.com" {
		t.Fatal("expected value to be re-encrypted with the client's key, not returned in plaintext")
	}
}

func TestListConfigsV2Endpoint_ReturnsFlatKeyValueMapEncryptedWithClientKey(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	apiKey := admin.createServiceClient(t)
	appName, appID := admin.createApplication(t)
	envName, envID := admin.createEnvironment(t, appID)
	admin.createConfig(t, appID, envID, "API_URL", "https://api.example.com")

	resp := apiRequest(t, base, http.MethodGet, "/api/v1/config/v2/configs/list?service="+appName+"&environment="+envName, apiKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	value, ok := body["API_URL"]
	if !ok {
		t.Fatalf("unexpected body: %+v", body)
	}
	if value == "https://api.example.com" {
		t.Fatal("expected value to be re-encrypted with the client's key, not returned in plaintext")
	}
}

// TestListConfigsV2Endpoint_MatchesV1Entries proves v2 is additive rather
// than a divergent read path: for the same service/environment/client, v2's
// flat map must contain exactly the same keys and (still-encrypted) values
// as v1's array, just reshaped.
func TestListConfigsV2Endpoint_MatchesV1Entries(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	apiKey := admin.createServiceClient(t)
	appName, appID := admin.createApplication(t)
	envName, envID := admin.createEnvironment(t, appID)
	admin.createConfig(t, appID, envID, "API_URL", "https://api.example.com")
	admin.createConfig(t, appID, envID, "TIMEOUT_MS", "3000")

	v1Resp := apiRequest(t, base, http.MethodGet, "/api/v1/config/configs/list?service="+appName+"&environment="+envName, apiKey, nil)
	defer v1Resp.Body.Close()
	if v1Resp.StatusCode != http.StatusOK {
		t.Fatalf("v1 status = %d", v1Resp.StatusCode)
	}
	var v1Body []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(v1Resp.Body).Decode(&v1Body); err != nil {
		t.Fatalf("decode v1: %v", err)
	}

	v2Resp := apiRequest(t, base, http.MethodGet, "/api/v1/config/v2/configs/list?service="+appName+"&environment="+envName, apiKey, nil)
	defer v2Resp.Body.Close()
	if v2Resp.StatusCode != http.StatusOK {
		t.Fatalf("v2 status = %d", v2Resp.StatusCode)
	}
	var v2Body map[string]string
	if err := json.NewDecoder(v2Resp.Body).Decode(&v2Body); err != nil {
		t.Fatalf("decode v2: %v", err)
	}

	if len(v1Body) != len(v2Body) {
		t.Fatalf("v1 has %d entries, v2 has %d entries: v1=%+v v2=%+v", len(v1Body), len(v2Body), v1Body, v2Body)
	}
	for _, entry := range v1Body {
		v2Value, ok := v2Body[entry.Key]
		if !ok {
			t.Fatalf("key %q present in v1 but missing from v2: %+v", entry.Key, v2Body)
		}
		if v2Value != entry.Value {
			t.Fatalf("key %q: v1 value %q != v2 value %q", entry.Key, entry.Value, v2Value)
		}
	}
}

func TestListConfigsV2Endpoint_UnknownScopeReturnsEmptyMap(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	apiKey := admin.createServiceClient(t)

	resp := apiRequest(t, base, http.MethodGet, "/api/v1/config/v2/configs/list?service=no-such-service&environment=no-such-env", apiKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("body = %+v, want empty map", body)
	}
}

func TestListConfigsV2Endpoint_RequiresAPIKeyAuth(t *testing.T) {
	base := e2eBaseURL(t)
	resp := apiRequest(t, base, http.MethodGet, "/api/v1/config/v2/configs/list?service=any&environment=any", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestListFeatureFlagsEndpoint_AcceptsServiceClientAPIKey(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	apiKey := admin.createServiceClient(t)
	appName, appID := admin.createApplication(t)
	envName, envID := admin.createEnvironment(t, appID)

	token := admin.csrfToken(t, "/flags/create/")
	resp, err := admin.http.PostForm(base+"/flags/create/", url.Values{
		"csrf_token":              {token},
		"application_id":          {appID},
		"environment_id":          {envID},
		"name":                    {"new-checkout"},
		"is_enabled":              {"on"},
		"create_all_environments": {""},
	})
	if err != nil {
		t.Fatalf("create flag: %v", err)
	}
	resp.Body.Close()

	listResp := apiRequest(t, base, http.MethodGet, "/api/v1/config/feature-flags?service="+appName+"&environment="+envName, apiKey, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", listResp.StatusCode)
	}
	var flags []struct {
		Name      string `json:"name"`
		IsEnabled bool   `json:"is_enabled"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&flags); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(flags) != 1 || flags[0].Name != "new-checkout" || !flags[0].IsEnabled {
		t.Fatalf("unexpected flags: %+v", flags)
	}
}

func TestAPIKeyAuth_FixedWindowThrottlesRepeatedRequests(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)
	apiKey := admin.createServiceClient(t)
	appName, appID := admin.createApplication(t)
	envName, _ := admin.createEnvironment(t, appID)

	path := "/api/v1/config/configs/list?service=" + appName + "&environment=" + envName
	var lastStatus int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp := apiRequest(t, base, http.MethodGet, path, apiKey, nil)
		lastStatus = resp.StatusCode
		resp.Body.Close()
		if lastStatus == http.StatusTooManyRequests {
			return
		}
	}
	t.Fatalf("expected a 429 within the rate limit window, last status = %d", lastStatus)
}
