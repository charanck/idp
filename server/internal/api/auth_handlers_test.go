package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/activity"
	"controlplane/internal/api"
	"controlplane/internal/auth"
	"controlplane/internal/jwtutil"
	"controlplane/internal/ratelimit"
	"controlplane/internal/testutil"
)

const testJWTSecret = "test-jwt-secret"

func setupAPI(t *testing.T) (*gorm.DB, *echo.Echo, *api.Deps) {
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

	deps := &api.Deps{
		AuthService:                auth.NewAuthService(gdb),
		Activity:                   activity.NewLogger(gdb),
		RateLimiter:                ratelimit.NewLimiter(rdb),
		JWTSecretKey:               testJWTSecret,
		JWTExpireMinutes:           60,
		AuthRateLimit:              1000,
		AuthRateLimitWindowSeconds: 60,
		S2SAuthRateLimit:           20,
	}

	e := echo.New()
	g := e.Group("/api/v1/auth")
	api.RegisterAuthRoutes(g, deps)

	return gdb, e, deps
}

func doJSON(e *echo.Echo, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func createAdminUser(t *testing.T, gdb *gorm.DB) *auth.User {
	t.Helper()
	user := &auth.User{Email: "admin@example.com", Username: "admin", IsStaff: true, IsActive: true}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	return user
}

func adminAuthHeader(t *testing.T, deps *api.Deps, user *auth.User) map[string]string {
	t.Helper()
	token, err := jwtutil.CreateAccessToken(deps.JWTSecretKey, user.ID.String(), "user", deps.JWTExpireMinutes)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestRegisterEndpoint_RequiresAdminAuth(t *testing.T) {
	_, e, _ := setupAPI(t)

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": "new@example.com", "password": "password123"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRegisterEndpoint_CreatesUserAsAdmin(t *testing.T) {
	gdb, e, deps := setupAPI(t)
	admin := createAdminUser(t, gdb)

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": "new@example.com", "password": "password123"}, adminAuthHeader(t, deps, admin))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Email    string `json:"email"`
		IsActive bool   `json:"is_active"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Email != "new@example.com" {
		t.Fatalf("email = %q", resp.Email)
	}
	if resp.IsActive {
		t.Fatal("expected new user to be inactive")
	}
}

func TestRegisterEndpoint_ConflictsOnDuplicateEmail(t *testing.T) {
	gdb, e, deps := setupAPI(t)
	admin := createAdminUser(t, gdb)
	if _, err := deps.AuthService.RegisterUser("dup@example.com", "password123", ""); err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": "dup@example.com", "password": "password123"}, adminAuthHeader(t, deps, admin))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestRegisterEndpoint_RejectsShortPassword(t *testing.T) {
	gdb, e, deps := setupAPI(t)
	admin := createAdminUser(t, gdb)

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": "new@example.com", "password": "short"}, adminAuthHeader(t, deps, admin))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestTokenEndpoint_IssuesTokenForValidCredentials(t *testing.T) {
	_, e, deps := setupAPI(t)
	user, err := deps.AuthService.RegisterUser("user@example.com", "password123", "")
	if err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}
	deps.AuthService.GetUserByID(user.ID) // no-op, keeps symmetry with the Python fixture setup

	// Activate the user directly (registration leaves accounts inactive).
	gdb := testutil.OpenDB(t)
	gdb.Model(&auth.User{}).Where("id = ?", user.ID).Update("is_active", true)

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/token", map[string]string{"username": "user@example.com", "password": "password123"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AccessToken == "" || resp.TokenType != "bearer" {
		t.Fatalf("unexpected token response: %+v", resp)
	}
}

func TestTokenEndpoint_RejectsInvalidCredentials(t *testing.T) {
	_, e, deps := setupAPI(t)
	if _, err := deps.AuthService.RegisterUser("user@example.com", "password123", ""); err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/token", map[string]string{"username": "user@example.com", "password": "wrong"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMeEndpoint_ReturnsCurrentUser(t *testing.T) {
	gdb, e, deps := setupAPI(t)
	user := &auth.User{Email: "user@example.com", Username: "user", IsActive: true}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := doJSON(e, http.MethodGet, "/api/v1/auth/me", nil, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Email string `json:"email"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Email != "user@example.com" {
		t.Fatalf("email = %q", resp.Email)
	}
}

func TestMeEndpoint_RequiresAuth(t *testing.T) {
	_, e, _ := setupAPI(t)
	rec := doJSON(e, http.MethodGet, "/api/v1/auth/me", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateServiceClient_RequiresAdmin(t *testing.T) {
	_, e, _ := setupAPI(t)
	rec := doJSON(e, http.MethodPost, "/api/v1/auth/s2s/clients", map[string]string{"name": "billing-service"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateServiceClient_ReturnsUsableAPIKey(t *testing.T) {
	gdb, e, deps := setupAPI(t)
	admin := createAdminUser(t, gdb)

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/s2s/clients", map[string]string{"name": "billing-service"}, adminAuthHeader(t, deps, admin))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		APIKeyID      string `json:"api_key_id"`
		APIKey        string `json:"api_key"`
		EncryptionKey string `json:"encryption_key"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.APIKey == "" || resp.EncryptionKey == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	client, err := deps.AuthService.AuthenticateServiceAPIKey(resp.APIKey)
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client == nil || client.Name != "billing-service" {
		t.Fatalf("expected a usable API key, got client %+v", client)
	}
}

func TestCreateServiceClient_ConflictsOnDuplicateName(t *testing.T) {
	gdb, e, deps := setupAPI(t)
	admin := createAdminUser(t, gdb)
	if _, err := deps.AuthService.CreateServiceClient("billing-service"); err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/auth/s2s/clients", map[string]string{"name": "billing-service"}, adminAuthHeader(t, deps, admin))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestS2SPing_RejectsInvalidAPIKey(t *testing.T) {
	_, e, _ := setupAPI(t)
	rec := doJSON(e, http.MethodGet, "/api/v1/auth/s2s/ping", nil, map[string]string{"X-API-Key": "sk_live_bad.wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestS2SPing_AcceptsValidAPIKey(t *testing.T) {
	_, e, deps := setupAPI(t)
	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	rec := doJSON(e, http.MethodGet, "/api/v1/auth/s2s/ping", nil, map[string]string{"X-API-Key": creds.APIKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeyAuth_RateLimitsRepeatedFailures(t *testing.T) {
	_, e, deps := setupAPI(t)
	deps.S2SAuthRateLimit = 2

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/s2s/ping", nil)
		req.Header.Set("X-API-Key", "sk_live_bad.wrong-secret")
		req.Header.Set("X-Forwarded-For", "10.0.0.5")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/s2s/ping", nil)
	req.Header.Set("X-API-Key", creds.APIKey)
	req.Header.Set("X-Forwarded-For", "10.0.0.5")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected valid key from throttled IP to still be blocked, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_DoesNotThrottleRepeatedValidKeyUsage(t *testing.T) {
	_, e, deps := setupAPI(t)
	deps.S2SAuthRateLimit = 2

	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/s2s/ping", nil)
		req.Header.Set("X-API-Key", creds.APIKey)
		req.Header.Set("X-Forwarded-For", "10.0.0.9")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200", i+1, rec.Code)
		}
	}
}
