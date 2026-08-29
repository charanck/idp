// Package e2e holds black-box HTTP tests that run against an already-running
// control-plane instance (started separately, e.g. via docker-compose + `go
// run ./cmd/server`), gated behind CP_E2E_BASE_URL. Unlike the rest of the
// repo's tests, these never construct services/handlers in-process - every
// request goes over the wire, exercising real routing, middleware, and
// worker wiring end to end.
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"time"

	"controlplane/internal/notification"
)

// e2eBaseURL returns the base URL of the running instance under test,
// skipping the calling test if CP_E2E_BASE_URL isn't set - mirroring the
// CP_TEST_DATABASE_URL skip pattern used for real-Postgres tests elsewhere.
func e2eBaseURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("CP_E2E_BASE_URL")
	if base == "" {
		t.Skip("CP_E2E_BASE_URL not set, skipping e2e test")
	}
	return strings.TrimRight(base, "/")
}

// skipIfNotificationDisabled skips the calling test when notification.Enabled
// is false, mirroring how the server itself skips registering the
// notification routes/worker - see internal/notification/flag.go. Without
// this, every notification e2e test would fail against the routes 404ing
// (via the login-required catch-all, since they're never registered) rather
// than exercising the feature.
func skipIfNotificationDisabled(t *testing.T) {
	t.Helper()
	if !notification.Enabled {
		t.Skip("notification.Enabled is false, skipping notification e2e test")
	}
}

var csrfTokenRe = regexp.MustCompile(`name="csrf_token" value="([^"]*)"`)

// adminSession is a cookie-carrying client logged in as the bootstrap admin,
// used to mint a fresh service client via the web UI for each test - there
// is no programmatic API for creating clients (see CLAUDE.md), so this is
// the only way an e2e test can obtain an X-API-Key.
type adminSession struct {
	base string
	http *http.Client
}

func newAdminSession(t *testing.T) *adminSession {
	t.Helper()
	base := e2eBaseURL(t)

	email := os.Getenv("ADMIN_EMAIL")
	password := os.Getenv("ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Skip("ADMIN_EMAIL/ADMIN_PASSWORD not set, skipping e2e test")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	s := &adminSession{base: base, http: &http.Client{Jar: jar}}

	token := s.csrfToken(t, "/login/")
	resp, err := s.http.PostForm(s.base+"/login/", url.Values{
		"csrf_token": {token},
		"username":   {email},
		"password":   {password},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want 200 or 302", resp.StatusCode)
	}

	return s
}

// mustAdminEmail returns ADMIN_EMAIL, skipping the calling test if it isn't
// set - mirrors the skip newAdminSession already does, for tests that need
// the bootstrap admin's own email rather than a fresh session.
func mustAdminEmail(t *testing.T) string {
	t.Helper()
	email := os.Getenv("ADMIN_EMAIL")
	if email == "" {
		t.Skip("ADMIN_EMAIL not set, skipping e2e test")
	}
	return email
}

// mustAdminPassword returns ADMIN_PASSWORD, skipping the calling test if it
// isn't set - see mustAdminEmail.
func mustAdminPassword(t *testing.T) string {
	t.Helper()
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		t.Skip("ADMIN_PASSWORD not set, skipping e2e test")
	}
	return password
}

// createUserReturningPassword is like createUser but also returns the fixed
// password it set, for tests that need to log in as the user afterward.
func (s *adminSession) createUserReturningPassword(t *testing.T) (email, password string) {
	t.Helper()
	email = fmt.Sprintf("e2e-user-%d@example.com", time.Now().UnixNano())
	password = "password12345"
	s.createUser(t, email, true)
	return email, password
}

// csrfToken GETs path and extracts the hidden csrf_token form field every
// session-authenticated form on the site embeds.
func (s *adminSession) csrfToken(t *testing.T, path string) string {
	t.Helper()
	resp, err := s.http.Get(s.base + path)
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

var apiKeyRe = regexp.MustCompile(`API Key:</strong> <code>([^<]+)</code>`)

// createServiceClient creates a uniquely-named service client via the web UI
// and returns its X-API-Key value.
func (s *adminSession) createServiceClient(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("e2e-notification-%d", time.Now().UnixNano())
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
	m := apiKeyRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no API key found in create-client response")
	}
	return string(m[1])
}

// apiRequest issues a request against the S2S JSON API (no cookies - API
// paths are stateless) and returns the raw response for the caller to
// inspect/close.
func apiRequest(t *testing.T, base, method, path, apiKey string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// TestNotificationSettingsEmailForm_SavesAndPrefillsStructuredFields drives
// the structured SMTP settings form (web/notification_settings_handler.go's
// editEmail) end to end over real HTTP/CSRF/session auth: save typed
// host/port/from/tls_mode/credentials, then GET the edit page again and
// confirm the non-secret fields round-trip while credentials are never
// echoed back into the form.
func TestNotificationSettingsEmailForm_SavesAndPrefillsStructuredFields(t *testing.T) {
	skipIfNotificationDisabled(t)
	s := newAdminSession(t)
	host := fmt.Sprintf("smtp-e2e-%d.example.com", time.Now().UnixNano())

	token := s.csrfToken(t, "/notification-settings/email/edit/")
	resp, err := s.http.PostForm(s.base+"/notification-settings/email/edit/", url.Values{
		"csrf_token": {token},
		"host":       {host},
		"port":       {"587"},
		"from":       {"noreply@example.com"},
		"from_name":  {"Example"},
		"tls_mode":   {"starttls"},
		"username":   {"smtp-user"},
		"password":   {"smtp-pass"},
		"is_active":  {"on"},
	})
	if err != nil {
		t.Fatalf("post email settings: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post email settings status = %d, want 200 (after redirect follow)", resp.StatusCode)
	}

	editResp, err := s.http.Get(s.base + "/notification-settings/email/edit/")
	if err != nil {
		t.Fatalf("get email edit page: %v", err)
	}
	defer editResp.Body.Close()
	body, err := io.ReadAll(editResp.Body)
	if err != nil {
		t.Fatalf("read email edit page: %v", err)
	}
	page := string(body)
	for _, want := range []string{host, "587", "noreply@example.com", "Example"} {
		if !strings.Contains(page, want) {
			t.Fatalf("email edit page missing prefilled %q\n%s", want, page)
		}
	}
	if strings.Contains(page, "smtp-pass") {
		t.Fatalf("email edit page must never echo back stored credentials")
	}
}

// TestNotificationSettingsEmailForm_RejectsInvalidPort exercises the
// structured form's server-side validation (not just client-side): an
// invalid port re-renders the form with 200 rather than persisting anything.
func TestNotificationSettingsEmailForm_RejectsInvalidPort(t *testing.T) {
	skipIfNotificationDisabled(t)
	s := newAdminSession(t)

	token := s.csrfToken(t, "/notification-settings/email/edit/")
	resp, err := s.http.PostForm(s.base+"/notification-settings/email/edit/", url.Values{
		"csrf_token": {token},
		"host":       {"smtp.example.com"},
		"port":       {"not-a-number"},
		"from":       {"noreply@example.com"},
		"tls_mode":   {"none"},
	})
	if err != nil {
		t.Fatalf("post email settings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered form)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(body), "Port must be a valid number") {
		t.Fatalf("response missing expected validation error:\n%s", body)
	}
}

// TestNotificationSettingsEmailForm_BlankCredentialsPreservesExisting saves
// credentials once, then saves again with blank username/password and
// confirms the channel is still reported "configured" on the list page -
// the "blank means unchanged" upsert convention holding over real HTTP.
func TestNotificationSettingsEmailForm_BlankCredentialsPreservesExisting(t *testing.T) {
	skipIfNotificationDisabled(t)
	s := newAdminSession(t)

	token := s.csrfToken(t, "/notification-settings/email/edit/")
	resp, err := s.http.PostForm(s.base+"/notification-settings/email/edit/", url.Values{
		"csrf_token": {token},
		"host":       {"smtp.example.com"},
		"port":       {"587"},
		"from":       {"noreply@example.com"},
		"tls_mode":   {"none"},
		"username":   {"first-user"},
		"password":   {"first-pass"},
	})
	if err != nil {
		t.Fatalf("post initial email settings: %v", err)
	}
	resp.Body.Close()

	token2 := s.csrfToken(t, "/notification-settings/email/edit/")
	resp2, err := s.http.PostForm(s.base+"/notification-settings/email/edit/", url.Values{
		"csrf_token": {token2},
		"host":       {"smtp.example.com"},
		"port":       {"2525"},
		"from":       {"noreply@example.com"},
		"tls_mode":   {"none"},
	})
	if err != nil {
		t.Fatalf("post follow-up email settings: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("follow-up status = %d, want 200 (after redirect follow)", resp2.StatusCode)
	}

	listResp, err := s.http.Get(s.base + "/notification-settings/")
	if err != nil {
		t.Fatalf("get notification settings list: %v", err)
	}
	defer listResp.Body.Close()
	body, err := io.ReadAll(listResp.Body)
	if err != nil {
		t.Fatalf("read list page: %v", err)
	}
	if !strings.Contains(string(body), "Configured") {
		t.Fatalf("email row expected to remain configured after blank-credentials save:\n%s", body)
	}

	editResp, err := s.http.Get(s.base + "/notification-settings/email/edit/")
	if err != nil {
		t.Fatalf("get email edit page: %v", err)
	}
	defer editResp.Body.Close()
	editBody, err := io.ReadAll(editResp.Body)
	if err != nil {
		t.Fatalf("read email edit page: %v", err)
	}
	if !strings.Contains(string(editBody), "2525") {
		t.Fatalf("config change from the follow-up save should have persisted:\n%s", editBody)
	}
}

func TestCreateNotificationEndpoint_RequiresAPIKeyAuth(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", "", []byte(`{"channel":"email","recipient":{},"content":{}}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateNotificationEndpoint_RejectsInvalidChannel(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)

	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey, []byte(`{"channel":"carrier-pigeon","recipient":{},"content":{}}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetNotificationEndpoint_ReturnsNotFoundForUnknownID(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)

	resp := apiRequest(t, base, http.MethodGet, "/api/v1/notifications/00000000-0000-0000-0000-000000000000", apiKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestNotificationLifecycle_CreateListGetAndDeliver drives a notification
// through the live create -> enqueue -> worker -> sent pipeline, exercising
// create, get, and list along the way. Uses "inapp" rather than "email":
// email now sends over real SMTP (see internal/notification/provider/email.go)
// and fails permanently when no provider settings are configured, whereas
// inapp has no external provider to fail against and always succeeds.
func TestNotificationLifecycle_CreateListGetAndDeliver(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"e2e-test","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}
	var created struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Channel != "inapp" {
		t.Fatalf("channel = %q, want inapp", created.Channel)
	}

	listResp := apiRequest(t, base, http.MethodGet, "/api/v1/notifications?channel=inapp", apiKey, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", listResp.StatusCode)
	}
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, n := range list {
		if n.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created notification %s not present in list %+v", created.ID, list)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		getResp := apiRequest(t, base, http.MethodGet, "/api/v1/notifications/"+created.ID, apiKey, nil)
		var got struct {
			Status            string `json:"status"`
			ProviderMessageID string `json:"provider_message_id"`
		}
		if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
			getResp.Body.Close()
			t.Fatalf("decode get response: %v", err)
		}
		getResp.Body.Close()
		if got.Status == "sent" {
			if got.ProviderMessageID == "" {
				t.Fatalf("sent notification missing provider_message_id")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for status=sent, last status = %q", got.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestCreateSessionEndpoint_MintsToken(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)

	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications/sessions", apiKey, []byte(`{"user_id":"e2e-user-1","service":"e2e-test"}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if body.Token == "" || body.ExpiresIn <= 0 {
		t.Fatalf("unexpected session response: %+v", body)
	}
}

func TestInAppUnreadEndpoint_RejectsMissingAuthorizationHeader(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	resp := apiRequest(t, base, http.MethodGet, "/api/v1/notifications/inapp/unread", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSSEEventsEndpoint_RejectsMissingAuthorizationHeader(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	resp := apiRequest(t, base, http.MethodGet, "/api/v1/notifications/sse/events", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// mintNotificationSessionToken is createSessionEndpoint's flow for the
// "e2e-test" application, reused by tests that need a bearer token
// authorizing them as userID against that application's notifications.
func mintNotificationSessionToken(t *testing.T, base, apiKey, userID string) string {
	t.Helper()
	return mintNotificationSessionTokenForService(t, base, apiKey, userID, "e2e-test")
}

// mintNotificationSessionTokenForService is mintNotificationSessionToken but
// lets the caller pick the application (service) the minted token is scoped
// to, for tests that need tokens scoped to distinct applications.
func mintNotificationSessionTokenForService(t *testing.T, base, apiKey, userID, service string) string {
	t.Helper()
	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications/sessions", apiKey, []byte(`{"user_id":"`+userID+`","service":"`+service+`"}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint session status = %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	return body.Token
}

func TestInAppUnreadEndpoint_ListsOnlyThatUsersUnreadAndMarksThemRead(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"e2e-test","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}

	token := mintNotificationSessionToken(t, base, apiKey, userID)

	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/notifications/inapp/unread", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	unreadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET inapp/unread: %v", err)
	}
	defer unreadResp.Body.Close()
	if unreadResp.StatusCode != http.StatusOK {
		t.Fatalf("unread status = %d", unreadResp.StatusCode)
	}
	var unread []struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(unreadResp.Body).Decode(&unread); err != nil {
		t.Fatalf("decode unread response: %v", err)
	}
	if len(unread) != 1 || unread[0].Channel != "inapp" {
		t.Fatalf("unread = %+v, want exactly one inapp notification", unread)
	}

	req2, err := http.NewRequest(http.MethodGet, base+"/api/v1/notifications/inapp/unread", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req2.Header.Set("Authorization", "Bearer "+token)
	againResp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET inapp/unread (again): %v", err)
	}
	defer againResp.Body.Close()
	var again []struct{}
	if err := json.NewDecoder(againResp.Body).Decode(&again); err != nil {
		t.Fatalf("decode second unread response: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second unread = %+v, want none left (first call should have marked them read)", again)
	}
}

// openSSEStream mints a session token for userID scoped to the "e2e-test"
// application and opens the live SSE stream, returning a channel that
// receives each event's "data: " payload as it arrives. The caller must
// close resp.Body (returned for that purpose) once done.
func openSSEStream(t *testing.T, base, apiKey, userID string) (events chan string, body io.ReadCloser) {
	t.Helper()
	return openSSEStreamForService(t, base, apiKey, userID, "e2e-test")
}

// openSSEStreamForService is openSSEStream but lets the caller pick the
// application (service) the stream's session token is scoped to.
func openSSEStreamForService(t *testing.T, base, apiKey, userID, service string) (events chan string, body io.ReadCloser) {
	t.Helper()
	token := mintNotificationSessionTokenForService(t, base, apiKey, userID, service)

	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/notifications/sse/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET sse/events: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("sse status = %d", resp.StatusCode)
	}

	events = make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if payload, ok := strings.CutPrefix(line, "data: "); ok {
				events <- payload
				return
			}
		}
	}()
	return events, resp.Body
}

// TestSSEEventsEndpoint_ReceivesDeliveryNotice subscribes to the live SSE
// stream for a fresh user, creates an inapp notification addressed to that
// user, and asserts the fire-and-forget delivery notice arrives once the
// worker processes it.
func TestSSEEventsEndpoint_ReceivesDeliveryNotice(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())

	events, body := openSSEStream(t, base, apiKey, userID)
	defer body.Close()

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"e2e-test","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	select {
	case payload := <-events:
		if !strings.Contains(payload, created.ID) || !strings.Contains(payload, "sent") {
			t.Fatalf("unexpected sse payload: %s", payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for sse delivery notice")
	}
}

// TestSSEEventsEndpoint_OnlyPublishesForInAppChannel asserts the SSE
// restriction added alongside real email sending: an email send (which now
// reaches a terminal status - sent or permanently failed - just like any
// other channel) must never produce an SSE event, only inapp sends do. This
// guards internal/notification/worker.go's Worker.publish gate.
func TestSSEEventsEndpoint_OnlyPublishesForInAppChannel(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())

	events, body := openSSEStream(t, base, apiKey, userID)
	defer body.Close()

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"e2e-test","channel":"email","recipient":{"email":"a@example.com","user_id":"`+userID+`"},"content":{"subject":"hi"}}`))
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}

	select {
	case payload := <-events:
		t.Fatalf("expected no sse event for an email send, got: %s", payload)
	case <-time.After(3 * time.Second):
		// Expected: no event within the window.
	}

	// Prove the stream itself is still alive and the hub still works, so the
	// absence above is the restriction working, not a dead subscription.
	createResp2 := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"e2e-test","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp2.Body.Close()
	if createResp2.StatusCode != http.StatusCreated {
		t.Fatalf("create inapp status = %d", createResp2.StatusCode)
	}
	var created2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp2.Body).Decode(&created2); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	select {
	case payload := <-events:
		if !strings.Contains(payload, created2.ID) {
			t.Fatalf("unexpected sse payload: %s", payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for sse delivery notice from inapp send")
	}
}

// TestCreateSessionEndpoint_RequiresService asserts the session-minting
// endpoint rejects a request missing "service" with 400, mirroring
// createNotificationRequest's own required-service validation - a session
// token with no application to scope it to must never be issued.
func TestCreateSessionEndpoint_RequiresService(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)

	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications/sessions", apiKey, []byte(`{"user_id":"e2e-user-1"}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestInAppUnreadEndpoint_ScopedByApplication_DoesNotLeakAcrossServices
// covers the application-scoping fix: the same user_id has an inapp
// notification created under application "a", but a session token minted
// for the same user_id under a different application "b" must not see it -
// only a token minted for application "a" should. This guards against a
// cross-tenant leak through a token that only carried user_id before.
func TestInAppUnreadEndpoint_ScopedByApplication_DoesNotLeakAcrossServices(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())
	serviceA := fmt.Sprintf("e2e-app-a-%d", time.Now().UnixNano())
	serviceB := fmt.Sprintf("e2e-app-b-%d", time.Now().UnixNano())

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"`+serviceA+`","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}

	// A token minted for the same user_id, but a different application, must
	// not surface application a's notification.
	tokenB := mintNotificationSessionTokenForService(t, base, apiKey, userID, serviceB)
	reqB, err := http.NewRequest(http.MethodGet, base+"/api/v1/notifications/inapp/unread", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	reqB.Header.Set("Authorization", "Bearer "+tokenB)
	unreadRespB, err := http.DefaultClient.Do(reqB)
	if err != nil {
		t.Fatalf("GET inapp/unread (service b): %v", err)
	}
	defer unreadRespB.Body.Close()
	if unreadRespB.StatusCode != http.StatusOK {
		t.Fatalf("unread (service b) status = %d", unreadRespB.StatusCode)
	}
	var unreadB []struct{}
	if err := json.NewDecoder(unreadRespB.Body).Decode(&unreadB); err != nil {
		t.Fatalf("decode unread (service b) response: %v", err)
	}
	if len(unreadB) != 0 {
		t.Fatalf("unread (service b) = %+v, want none - notification belongs to a different application", unreadB)
	}

	// A token minted for the matching application must see it.
	tokenA := mintNotificationSessionTokenForService(t, base, apiKey, userID, serviceA)
	reqA, err := http.NewRequest(http.MethodGet, base+"/api/v1/notifications/inapp/unread", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	reqA.Header.Set("Authorization", "Bearer "+tokenA)
	unreadRespA, err := http.DefaultClient.Do(reqA)
	if err != nil {
		t.Fatalf("GET inapp/unread (service a): %v", err)
	}
	defer unreadRespA.Body.Close()
	if unreadRespA.StatusCode != http.StatusOK {
		t.Fatalf("unread (service a) status = %d", unreadRespA.StatusCode)
	}
	var unreadA []struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(unreadRespA.Body).Decode(&unreadA); err != nil {
		t.Fatalf("decode unread (service a) response: %v", err)
	}
	if len(unreadA) != 1 || unreadA[0].Channel != "inapp" {
		t.Fatalf("unread (service a) = %+v, want exactly one inapp notification", unreadA)
	}
}

// TestSSEEventsEndpoint_ScopedByApplication_DoesNotLeakAcrossServices is the
// SSE-stream counterpart to
// TestInAppUnreadEndpoint_ScopedByApplication_DoesNotLeakAcrossServices: a
// stream opened with a token scoped to application "b" must not receive an
// event published for the same user_id's notification under application
// "a", proving sseChannelName's application-scoped Redis channel naming
// actually isolates the two.
func TestSSEEventsEndpoint_ScopedByApplication_DoesNotLeakAcrossServices(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())
	serviceA := fmt.Sprintf("e2e-app-a-%d", time.Now().UnixNano())
	serviceB := fmt.Sprintf("e2e-app-b-%d", time.Now().UnixNano())

	eventsB, bodyB := openSSEStreamForService(t, base, apiKey, userID, serviceB)
	defer bodyB.Close()

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"`+serviceA+`","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}

	select {
	case payload := <-eventsB:
		t.Fatalf("expected no sse event on a different application's stream, got: %s", payload)
	case <-time.After(3 * time.Second):
		// Expected: no event within the window.
	}

	// Prove the notification really did send, and that a stream scoped to
	// the matching application does receive it - so the absence above is the
	// scoping working, not a dead subscription or an undelivered send.
	eventsA, bodyA := openSSEStreamForService(t, base, apiKey, userID, serviceA)
	defer bodyA.Close()

	createResp2 := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"service":"`+serviceA+`","channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
	defer createResp2.Body.Close()
	if createResp2.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp2.StatusCode)
	}
	var created2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp2.Body).Decode(&created2); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	select {
	case payload := <-eventsA:
		if !strings.Contains(payload, created2.ID) {
			t.Fatalf("unexpected sse payload: %s", payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for sse delivery notice on the matching application's stream")
	}
}
