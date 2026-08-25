package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/config"
	"controlplane/internal/crypto"
)

func newUnitConfigService(t *testing.T) (*config.ConfigService, *fakeApplicationRepository, *fakeEnvironmentRepository, *fakeConfigRepository) {
	t.Helper()
	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	apps := newFakeApplicationRepository()
	envs := newFakeEnvironmentRepository(apps)
	configs := newFakeConfigRepository(apps, envs)
	svc := config.NewConfigService(configs, apps, envs, crypto.NewEncryptionService(masterKey), newFakeCache(), time.Minute)
	return svc, apps, envs, configs
}

func TestUpsertConfig_CreatesFirstVersionOnFirstWrite(t *testing.T) {
	svc, _, _, configs := newUnitConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://api.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	versions, err := configs.ListVersions(ctx, entry.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0].Version != 1 || versions[0].Action != config.ActionCreate {
		t.Fatalf("unexpected first version: %+v", versions[0])
	}
}

func TestUpsertConfig_UpdateBumpsVersionNumber(t *testing.T) {
	svc, _, _, configs := newUnitConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig (create): %v", err)
	}
	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v2", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig (update): %v", err)
	}

	versions, err := configs.ListVersions(ctx, entry.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[0].Action != config.ActionUpdate {
		t.Fatalf("unexpected latest version: %+v", versions[0])
	}
	if versions[1].Version != 1 {
		t.Fatalf("unexpected first version: %+v", versions[1])
	}
}

func TestUpsertConfig_ReusesExistingApplicationAndEnvironment(t *testing.T) {
	svc, apps, envs, _ := newUnitConfigService(t)
	ctx := context.Background()

	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "A", "1", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "B", "2", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	appList, _ := apps.List(ctx, "")
	if len(appList) != 1 {
		t.Fatalf("expected exactly 1 application, got %d", len(appList))
	}
	envList, _ := envs.ListByApplicationID(ctx, appList[0].ID)
	if len(envList) != 1 {
		t.Fatalf("expected exactly 1 environment, got %d", len(envList))
	}
}

func TestGetConfig_ReturnsNilForUnknownScope_FakeRepo(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	got, err := svc.GetConfig(context.Background(), "unknown", "prod", "KEY")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestDeleteConfig_RemovesEntryAndReturnsTrue(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	deleted, err := svc.DeleteConfig(ctx, entry.ID.String())
	if err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleted = true")
	}

	got, err := svc.GetConfigByID(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetConfigByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected entry to be gone")
	}
}

func TestDeleteConfig_ReturnsFalseForUnknownID_FakeRepo(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	deleted, err := svc.DeleteConfig(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if deleted {
		t.Fatal("expected deleted = false")
	}
}

func TestDeleteConfig_ReturnsFalseForMalformedID_FakeRepo(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	deleted, err := svc.DeleteConfig(context.Background(), "not-a-uuid")
	if err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if deleted {
		t.Fatal("expected deleted = false")
	}
}

func TestRollbackConfig_RestoresPriorValueAsNewVersion_FakeRepo(t *testing.T) {
	svc, _, _, configs := newUnitConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig (v1): %v", err)
	}
	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v2", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig (v2): %v", err)
	}

	rolledBack, err := svc.RollbackConfig(ctx, entry.ID.String(), 1, "admin@example.com")
	if err != nil {
		t.Fatalf("RollbackConfig: %v", err)
	}
	if rolledBack == nil {
		t.Fatal("expected non-nil rollback result")
	}

	decrypted, err := svc.DecryptConfigValue(rolledBack)
	if err != nil {
		t.Fatalf("DecryptConfigValue: %v", err)
	}
	if decrypted != "v1" {
		t.Fatalf("decrypted = %q, want v1", decrypted)
	}

	versions, err := configs.ListVersions(ctx, entry.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions after rollback, got %d", len(versions))
	}
	if versions[0].Version != 3 || versions[0].Action != config.ActionRollback {
		t.Fatalf("unexpected latest version: %+v", versions[0])
	}
}

func TestRollbackConfig_ReturnsNilForUnknownVersion_FakeRepo(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v1", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	got, err := svc.RollbackConfig(ctx, entry.ID.String(), 99, "admin@example.com")
	if err != nil {
		t.Fatalf("RollbackConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for unknown version")
	}
}

func TestListConfigsForClient_ServesFromCacheOnSecondCall(t *testing.T) {
	svc, _, _, configs := newUnitConfigService(t)
	ctx := context.Background()

	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://api.example.com", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	clientKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	first, err := svc.ListConfigsForClient(ctx, "payments", "prod", clientKey)
	if err != nil {
		t.Fatalf("ListConfigsForClient (1st): %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 config, got %d", len(first))
	}

	configs.listByScopeCalls = 0
	second, err := svc.ListConfigsForClient(ctx, "payments", "prod", clientKey)
	if err != nil {
		t.Fatalf("ListConfigsForClient (2nd): %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 config from cache, got %d", len(second))
	}
	if configs.listByScopeCalls != 0 {
		t.Fatalf("expected ListByScope not to be called on a cache hit, got %d calls", configs.listByScopeCalls)
	}
}

func TestListConfigsForClient_CacheInvalidatedByNewWrite(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	ctx := context.Background()

	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v1", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	clientKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	if _, err := svc.ListConfigsForClient(ctx, "payments", "prod", clientKey); err != nil {
		t.Fatalf("ListConfigsForClient (1st): %v", err)
	}
	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "SECOND_KEY", "v1", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig (2nd key): %v", err)
	}

	second, err := svc.ListConfigsForClient(ctx, "payments", "prod", clientKey)
	if err != nil {
		t.Fatalf("ListConfigsForClient (2nd): %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("expected 2 configs after invalidation, got %d", len(second))
	}
}

func TestUpdateConfigEntry_ReturnsNilForUnknownID(t *testing.T) {
	svc, _, _, _ := newUnitConfigService(t)
	got, err := svc.UpdateConfigEntry(context.Background(), uuid.New(), config.UpdateConfigEntryInput{})
	if err != nil {
		t.Fatalf("UpdateConfigEntry: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}
