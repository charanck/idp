package notification_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/crypto"
	"controlplane/internal/notification"
	"controlplane/internal/ratelimit"
	"controlplane/internal/testutil"
)

func setupNotificationAPI(t *testing.T) (*gorm.DB, *echo.Echo, *notification.Deps, *auth.AuthService) {
	t.Helper()
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	authService := auth.NewAuthService(gdb)
	enqueuer := &fakeEnqueuer{}
	notifications := notification.NewNotificationService(gdb, enqueuer)

	deps := &notification.Deps{
		Authenticator:              auth.ServiceClientAuthenticator{Service: authService},
		Notifications:              notifications,
		Channels:                   notification.NewChannelRegistry(),
		RateLimiter:                ratelimit.NewLimiter(rdb),
		Hub:                        notification.NewHub(rdb),
		TokenIssuer:                notification.NewTokenIssuer(newTestEncryption(t)),
		AuthRateLimitWindowSeconds: 60,
		S2SAuthRateLimit:           1000,
	}

	e := echo.New()
	group := e.Group("/api/v1/notifications")
	notification.RegisterRoutes(group, deps)

	return gdb, e, deps, authService
}

func doNotificationRequest(e *echo.Echo, method, path string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestCreateNotificationEndpoint_RequiresAPIKeyAuth(t *testing.T) {
	_, e, _, _ := setupNotificationAPI(t)
	rec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications", nil, []byte(`{"channel":"email","recipient":{},"content":{}}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateNotificationEndpoint_CreatesAndReturnsQueued(t *testing.T) {
	_, e, _, authService := setupNotificationAPI(t)
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	body := []byte(`{"channel":"email","recipient":{"email":"a@example.com"},"content":{"subject":"hi"}}`)
	rec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications", map[string]string{"X-API-Key": creds.APIKey}, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Channel != "email" || resp.Status != notification.StatusQueued {
		t.Fatalf("unexpected response: %+v", resp)
	}

	getRec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications/"+resp.ID, map[string]string{"X-API-Key": creds.APIKey}, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
}

func TestCreateNotificationEndpoint_RejectsInvalidChannel(t *testing.T) {
	_, e, _, authService := setupNotificationAPI(t)
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	rec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications", map[string]string{"X-API-Key": creds.APIKey}, []byte(`{"channel":"carrier-pigeon","recipient":{},"content":{}}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListNotificationsEndpoint_ReturnsCreatedNotifications(t *testing.T) {
	_, e, _, authService := setupNotificationAPI(t)
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	createRec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications", map[string]string{"X-API-Key": creds.APIKey}, []byte(`{"channel":"sms","recipient":{"phone":"+1"},"content":{"body":"hi"}}`))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	listRec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications?channel=sms", map[string]string{"X-API-Key": creds.APIKey}, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var list []struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list) != 1 || list[0].Channel != "sms" {
		t.Fatalf("list = %+v", list)
	}
}

func TestGetNotificationEndpoint_ReturnsNotFoundForUnknownID(t *testing.T) {
	_, e, _, authService := setupNotificationAPI(t)
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	rec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications/00000000-0000-0000-0000-000000000000", map[string]string{"X-API-Key": creds.APIKey}, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCreateSessionEndpoint_MintsToken(t *testing.T) {
	_, e, _, authService := setupNotificationAPI(t)
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	rec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications/sessions", map[string]string{"X-API-Key": creds.APIKey}, []byte(`{"user_id":"user-1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Token == "" || resp.ExpiresIn <= 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestInAppUnreadEndpoint_RejectsMissingAuthorizationHeader(t *testing.T) {
	_, e, _, _ := setupNotificationAPI(t)
	rec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications/inapp/unread", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSSEEventsEndpoint_RejectsMissingAuthorizationHeader(t *testing.T) {
	_, e, _, _ := setupNotificationAPI(t)
	rec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications/sse/events", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestInAppUnreadEndpoint_ListsOnlyThatUsersUnreadAndMarksThemRead(t *testing.T) {
	_, e, _, authService := setupNotificationAPI(t)
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	createRec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications",
		map[string]string{"X-API-Key": creds.APIKey},
		[]byte(`{"channel":"inapp","recipient":{"user_id":"user-1"},"content":{"title":"hi"}}`))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	tokenRec := doNotificationRequest(e, http.MethodPost, "/api/v1/notifications/sessions",
		map[string]string{"X-API-Key": creds.APIKey}, []byte(`{"user_id":"user-1"}`))
	if tokenRec.Code != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", tokenRec.Code, tokenRec.Body.String())
	}
	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(tokenRec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	authHeader := map[string]string{"Authorization": "Bearer " + tokenResp.Token}

	unreadRec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications/inapp/unread", authHeader, nil)
	if unreadRec.Code != http.StatusOK {
		t.Fatalf("unread status = %d, body = %s", unreadRec.Code, unreadRec.Body.String())
	}
	var unread []struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(unreadRec.Body.Bytes(), &unread); err != nil {
		t.Fatalf("unmarshal unread: %v", err)
	}
	if len(unread) != 1 || unread[0].Channel != "inapp" {
		t.Fatalf("unread = %+v", unread)
	}

	// A second call should see nothing left unread - the first call marked
	// what it returned as read.
	againRec := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications/inapp/unread", authHeader, nil)
	if againRec.Code != http.StatusOK {
		t.Fatalf("second unread status = %d, body = %s", againRec.Code, againRec.Body.String())
	}
	var again []struct{}
	if err := json.Unmarshal(againRec.Body.Bytes(), &again); err != nil {
		t.Fatalf("unmarshal second unread: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second unread = %+v, want none left", again)
	}
}

func TestAPIKeyAuth_UsesSeparateRateLimitBucketFromConfigAPI(t *testing.T) {
	_, e, deps, authService := setupNotificationAPI(t)
	deps.S2SAuthRateLimit = 1
	creds, err := authService.CreateServiceClient(context.Background(), "notifier-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	headers := map[string]string{"X-API-Key": creds.APIKey, "X-Forwarded-For": "10.0.0.9"}
	first := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications", headers, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := doNotificationRequest(e, http.MethodGet, "/api/v1/notifications", headers, nil)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
}

func newTestEncryption(t *testing.T) *crypto.EncryptionService {
	t.Helper()
	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return crypto.NewEncryptionService(masterKey)
}
