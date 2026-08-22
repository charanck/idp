package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/activity"
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
		Activity:                   activity.NewLogger(gdb),
		RateLimiter:                ratelimit.NewLimiter(rdb),
		JWTSecretKey:               testJWTSecret,
		JWTExpireMinutes:           60,
		AuthRateLimit:              1000,
		AuthRateLimitWindowSeconds: 60,
		S2SAuthRateLimit:           20,
	}

	e := echo.New()
	authGroup := e.Group("/api/v1/auth")
	api.RegisterAuthRoutes(authGroup, deps)
	configGroup := e.Group("/api/v1/config")
	api.RegisterConfigRoutes(configGroup, deps)

	return gdb, e, deps
}

func createActiveUser(t *testing.T, gdb *gorm.DB) *auth.User {
	t.Helper()
	user := &auth.User{Email: "user@example.com", Username: "user", IsActive: true}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func TestUpsertConfigEndpoint_RequiresJWTAuth(t *testing.T) {
	_, e, _ := setupConfigAPI(t)
	rec := doJSON(e, http.MethodPost, "/api/v1/config/configs/upsert", map[string]any{
		"service": "payments", "environment": "prod", "key": "API_URL", "value": "https://a",
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestUpsertConfigEndpoint_CreatesConfigAndNeverReturnsRawValue(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	rec := doJSON(e, http.MethodPost, "/api/v1/config/configs/upsert", map[string]any{
		"service": "payments", "environment": "prod", "key": "API_URL",
		"value": "https://api.example.com", "is_secret": true,
	}, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Value != "***ENCRYPTED***" || !body.IsSecret {
		t.Fatalf("unexpected body: %+v", body)
	}

	stored, err := deps.ConfigService.GetConfig("payments", "prod", "API_URL")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	decrypted, err := deps.ConfigService.DecryptConfigValue(stored)
	if err != nil {
		t.Fatalf("DecryptConfigValue: %v", err)
	}
	if decrypted != "https://api.example.com" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestUpsertConfigEndpoint_UpsertingSameKeyUpdatesRatherThanDuplicates(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)
	headers := adminAuthHeader(t, deps, user)

	payload := map[string]any{"service": "payments", "environment": "prod", "key": "API_URL", "value": "https://v1"}
	doJSON(e, http.MethodPost, "/api/v1/config/configs/upsert", payload, headers)

	payload["value"] = "https://v2"
	doJSON(e, http.MethodPost, "/api/v1/config/configs/upsert", payload, headers)

	stored, err := deps.ConfigService.GetConfig("payments", "prod", "API_URL")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	decrypted, err := deps.ConfigService.DecryptConfigValue(stored)
	if err != nil {
		t.Fatalf("DecryptConfigValue: %v", err)
	}
	if decrypted != "https://v2" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestConfigHistoryEndpoint_RequiresJWTAuth(t *testing.T) {
	_, e, deps := setupConfigAPI(t)
	entry, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodGet, "/api/v1/config/configs/"+entry.ID.String()+"/history", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestConfigHistoryEndpoint_ListsVersionsNewestFirstWithDecryptedValues(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	entry, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v2", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodGet, "/api/v1/config/configs/"+entry.ID.String()+"/history", nil, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body []struct {
		Action string  `json:"action"`
		Value  *string `json:"value"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body) != 2 || body[0].Action != "update" || body[1].Action != "create" {
		t.Fatalf("unexpected history: %+v", body)
	}
	if body[0].Value == nil || *body[0].Value != "https://v2" {
		t.Fatalf("body[0].Value = %v", body[0].Value)
	}
	if body[1].Value == nil || *body[1].Value != "https://v1" {
		t.Fatalf("body[1].Value = %v", body[1].Value)
	}
}

func TestConfigHistoryEndpoint_NeverExposesSecretValues(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	entry, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "DB_PASSWORD", "s3cret", config.UpsertOptions{IsSecret: true})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodGet, "/api/v1/config/configs/"+entry.ID.String()+"/history", nil, adminAuthHeader(t, deps, user))
	var body []struct {
		IsSecret bool    `json:"is_secret"`
		Value    *string `json:"value"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body) != 1 || !body[0].IsSecret || body[0].Value != nil {
		t.Fatalf("unexpected history: %+v", body)
	}
}

func TestRollbackEndpoint_RestoresPriorValue(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	entry, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v2", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/config/configs/"+entry.ID.String()+"/rollback", map[string]any{"version": 1}, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Value string `json:"value"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Value != "***ENCRYPTED***" {
		t.Fatalf("value = %q", body.Value)
	}

	stored, err := deps.ConfigService.GetConfig("payments", "prod", "API_URL")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	decrypted, err := deps.ConfigService.DecryptConfigValue(stored)
	if err != nil {
		t.Fatalf("DecryptConfigValue: %v", err)
	}
	if decrypted != "https://v1" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestRollbackEndpoint_ReturnsNotFoundForUnknownVersion(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	entry, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/config/configs/"+entry.ID.String()+"/rollback", map[string]any{"version": 99}, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRollbackEndpoint_RequiresJWTAuth(t *testing.T) {
	_, e, deps := setupConfigAPI(t)
	entry, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "API_URL", "https://v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/config/configs/"+entry.ID.String()+"/rollback", map[string]any{"version": 1}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestListConfigsForClientEndpoint_RequiresAPIKeyAuth(t *testing.T) {
	_, e, _ := setupConfigAPI(t)
	rec := doJSON(e, http.MethodGet, "/api/v1/config/configs/list?service=payments&environment=prod", nil, nil)
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

	rec := doJSON(e, http.MethodGet, "/api/v1/config/configs/list?service=payments&environment=prod", nil, map[string]string{"X-API-Key": creds.APIKey})
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

	rec := doJSON(e, http.MethodGet, "/api/v1/config/configs/list?service=unknown&environment=prod", nil, map[string]string{"X-API-Key": creds.APIKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCreateFeatureFlagEndpoint_CreatesAcrossAllEnvironments(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "seed", "v", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "staging", "seed", "v", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	rec := doJSON(e, http.MethodPost, "/api/v1/config/feature-flags", map[string]any{
		"service": "payments", "name": "new-checkout", "is_enabled": true,
	}, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		CreatedCount int `json:"created_count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.CreatedCount != 2 {
		t.Fatalf("created_count = %d", body.CreatedCount)
	}
}

func TestCreateFeatureFlagEndpoint_ConflictsWhenApplicationUnknown(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	rec := doJSON(e, http.MethodPost, "/api/v1/config/feature-flags", map[string]any{
		"service": "unknown-app", "name": "new-checkout",
	}, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListAndToggleFeatureFlagEndpoint_Roundtrip(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)
	headers := adminAuthHeader(t, deps, user)
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "seed", "v", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	doJSON(e, http.MethodPost, "/api/v1/config/feature-flags", map[string]any{
		"service": "payments", "environment": "prod", "name": "new-checkout", "is_enabled": false,
	}, headers)

	listRec := doJSON(e, http.MethodGet, "/api/v1/config/feature-flags?service=payments&environment=prod", nil, headers)
	if listRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listBody []struct {
		IsEnabled bool `json:"is_enabled"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &listBody)
	if len(listBody) != 1 || listBody[0].IsEnabled {
		t.Fatalf("unexpected list body: %+v", listBody)
	}

	toggleRec := doJSON(e, http.MethodPost, "/api/v1/config/feature-flags/new-checkout/toggle?service=payments&environment=prod", nil, headers)
	if toggleRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", toggleRec.Code, toggleRec.Body.String())
	}
	var toggleBody struct {
		IsEnabled bool `json:"is_enabled"`
	}
	json.Unmarshal(toggleRec.Body.Bytes(), &toggleBody)
	if !toggleBody.IsEnabled {
		t.Fatal("expected enabled after toggle")
	}
}

func TestToggleFeatureFlagEndpoint_ReturnsNotFoundForUnknownFlag(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)

	rec := doJSON(e, http.MethodPost, "/api/v1/config/feature-flags/missing/toggle?service=payments&environment=prod", nil, adminAuthHeader(t, deps, user))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListFeatureFlagsEndpoint_AcceptsServiceClientAPIKey(t *testing.T) {
	gdb, e, deps := setupConfigAPI(t)
	user := createActiveUser(t, gdb)
	headers := adminAuthHeader(t, deps, user)
	creds, err := deps.AuthService.CreateServiceClient("billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	if _, err := deps.ConfigService.UpsertConfig(context.Background(), "payments", "prod", "seed", "v", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	doJSON(e, http.MethodPost, "/api/v1/config/feature-flags", map[string]any{
		"service": "payments", "environment": "prod", "name": "new-checkout", "is_enabled": true,
	}, headers)

	rec := doJSON(e, http.MethodGet, "/api/v1/config/feature-flags?service=payments&environment=prod", nil, map[string]string{"X-API-Key": creds.APIKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body []struct {
		Name string `json:"name"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body) != 1 || body[0].Name != "new-checkout" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestListFeatureFlagsEndpoint_RejectsMissingAuth(t *testing.T) {
	_, e, _ := setupConfigAPI(t)
	rec := doJSON(e, http.MethodGet, "/api/v1/config/feature-flags?service=payments&environment=prod", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
