package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/api"
	"controlplane/internal/auth"
	"controlplane/internal/cache"
	"controlplane/internal/config"
	"controlplane/internal/crypto"
	"controlplane/internal/ratelimit"
	"controlplane/internal/testutil"
)

func setupConfigAPI(t *testing.T) (*gorm.DB, *echo.Echo, *api.Deps) {
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
	c := cache.NewRedisCache(rdb, "")

	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	enc := crypto.NewEncryptionService(masterKey)

	deps := &api.Deps{
		AuthService:                auth.NewAuthService(gdb),
		ConfigService:              config.NewConfigService(gdb, enc, c, 5*time.Minute),
		FlagService:                config.NewFeatureFlagService(gdb, c, 5*time.Minute),
		RateLimiter:                ratelimit.NewLimiter(rdb),
		AuthRateLimitWindowSeconds: 60,
		S2SAuthRateLimit:           1000,
	}

	e := echo.New()
	configGroup := e.Group("/api/v1/config")
	api.RegisterConfigRoutes(configGroup, deps)

	return gdb, e, deps
}

func doGet(e *echo.Echo, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestListConfigsForClientEndpoint_RequiresAPIKeyAuth(t *testing.T) {
	_, e, _ := setupConfigAPI(t)
	rec := doGet(e, "/api/v1/config/configs/list?service=payments&environment=prod", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestListConfigsForClientEndpoint_ReturnsConfigsEncryptedWithClientKey(t *testing.T) {
	_, e, deps := setupConfigAPI(t)
	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://api.example.com", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doGet(e, "/api/v1/config/configs/list?service=payments&environment=prod", map[string]string{"X-API-Key": creds.APIKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body) != 1 || body[0].Key != "API_URL" {
		t.Fatalf("unexpected body: %+v", body)
	}

	decrypted, err := crypto.DecryptValue(body[0].Value, creds.Client.EncryptionKey)
	if err != nil {
		t.Fatalf("decrypt with client key: %v", err)
	}
	if decrypted != "https://api.example.com" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestListConfigsForClientEndpoint_ReturnsEmptyListForUnknownScope(t *testing.T) {
	_, e, deps := setupConfigAPI(t)
	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	rec := doGet(e, "/api/v1/config/configs/list?service=unknown&environment=prod", map[string]string{"X-API-Key": creds.APIKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestListFeatureFlagsEndpoint_AcceptsServiceClientAPIKey(t *testing.T) {
	_, e, deps := setupConfigAPI(t)
	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "seed", "v", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if _, err := deps.FlagService.CreateFlag(context.Background(), "payments", "new-checkout", config.CreateFlagOptions{
		IsEnabled: true, Environment: "prod", CreateAllEnvironments: true,
	}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	rec := doGet(e, "/api/v1/config/feature-flags?service=payments&environment=prod", map[string]string{"X-API-Key": creds.APIKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body []struct {
		Name      string `json:"name"`
		IsEnabled bool   `json:"is_enabled"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body) != 1 || body[0].Name != "new-checkout" || !body[0].IsEnabled {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestListFeatureFlagsEndpoint_RejectsMissingAuth(t *testing.T) {
	_, e, _ := setupConfigAPI(t)
	rec := doGet(e, "/api/v1/config/feature-flags?service=payments&environment=prod", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyAuth_FixedWindowThrottlesRepeatedRequests(t *testing.T) {
	_, e, deps := setupConfigAPI(t)
	deps.S2SAuthRateLimit = 2
	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	headers := map[string]string{"X-API-Key": creds.APIKey, "X-Forwarded-For": "10.0.0.5"}
	for i := 0; i < 2; i++ {
		rec := doGet(e, "/api/v1/config/configs/list?service=payments&environment=prod", headers)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200", i+1, rec.Code)
		}
	}

	rec := doGet(e, "/api/v1/config/configs/list?service=payments&environment=prod", headers)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: status = %d, want 429", rec.Code)
	}
}
