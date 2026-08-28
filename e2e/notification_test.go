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
// create, get, and list along the way.
func TestNotificationLifecycle_CreateListGetAndDeliver(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"channel":"email","recipient":{"email":"a@example.com"},"content":{"subject":"hi"}}`))
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
	if created.Channel != "email" {
		t.Fatalf("channel = %q, want email", created.Channel)
	}

	listResp := apiRequest(t, base, http.MethodGet, "/api/v1/notifications?channel=email", apiKey, nil)
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

	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications/sessions", apiKey, []byte(`{"user_id":"e2e-user-1"}`))
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

// mintNotificationSessionToken is createSessionEndpoint's flow, reused by
// tests that need a bearer token authorizing them as userID.
func mintNotificationSessionToken(t *testing.T, base, apiKey, userID string) string {
	t.Helper()
	resp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications/sessions", apiKey, []byte(`{"user_id":"`+userID+`"}`))
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
		[]byte(`{"channel":"inapp","recipient":{"user_id":"`+userID+`"},"content":{"title":"hi"}}`))
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

// TestSSEEventsEndpoint_ReceivesDeliveryNotice subscribes to the live SSE
// stream for a fresh user, creates a notification addressed to that user,
// and asserts the fire-and-forget delivery notice arrives once the worker
// processes it.
func TestSSEEventsEndpoint_ReceivesDeliveryNotice(t *testing.T) {
	skipIfNotificationDisabled(t)
	base := e2eBaseURL(t)
	apiKey := newAdminSession(t).createServiceClient(t)
	userID := fmt.Sprintf("e2e-user-%d", time.Now().UnixNano())
	token := mintNotificationSessionToken(t, base, apiKey, userID)

	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/notifications/sse/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET sse/events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d", resp.StatusCode)
	}

	events := make(chan string, 1)
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

	createResp := apiRequest(t, base, http.MethodPost, "/api/v1/notifications", apiKey,
		[]byte(`{"channel":"email","recipient":{"email":"a@example.com","user_id":"`+userID+`"},"content":{"subject":"hi"}}`))
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
