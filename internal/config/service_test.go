package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/config"
	"controlplane/internal/crypto"
)

func newTestConfigService(t *testing.T) (*config.ConfigService, *fakeApplicationRepository, *fakeEnvironmentRepository, *fakeConfigRepository) {
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
	svc, _, _, configs := newTestConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "v1", config.UpsertOptions{})
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
	svc, _, _, configs := newTestConfigService(t)
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
	svc, apps, envs, _ := newTestConfigService(t)
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

func TestUpsertConfig_StoresValueEncryptedNotInPlaintext(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()

	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://api.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if entry.Value == "https://api.example.com" {
		t.Fatal("expected stored value to be encrypted")
	}
	decrypted, err := svc.DecryptConfigValue(entry)
	if err != nil {
		t.Fatalf("DecryptConfigValue: %v", err)
	}
	if decrypted != "https://api.example.com" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestUpsertConfig_UpdatesExistingEntryInsteadOfDuplicating(t *testing.T) {
	svc, _, _, configs := newTestConfigService(t)
	ctx := context.Background()

	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v1.example.com", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig 1: %v", err)
	}
	updated, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v2.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig 2: %v", err)
	}

	entries, err := configs.ListByScope(ctx, updated.ApplicationID, updated.EnvironmentID)
	if err != nil {
		t.Fatalf("ListByScope: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry, got %d", len(entries))
	}

	decrypted, _ := svc.DecryptConfigValue(updated)
	if decrypted != "https://v2.example.com" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestGetConfig_ReturnsNilForUnknownScope(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	got, err := svc.GetConfig(context.Background(), "unknown-app", "prod", "KEY")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestGetConfig_ReturnsNilForUnknownKeyInKnownScope(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "KEY_A", "a", config.UpsertOptions{})

	got, err := svc.GetConfig(ctx, "payments", "prod", "KEY_B")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestGetConfig_FindsExistingEntry(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "KEY_A", "a", config.UpsertOptions{})

	found, err := svc.GetConfig(ctx, "payments", "prod", "KEY_A")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if found == nil || found.Key != "KEY_A" {
		t.Fatalf("got %+v", found)
	}
}

func TestListConfigs_ScopesToServiceAndEnvironment(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "KEY_A", "a", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "payments", "staging", "KEY_A", "a-staging", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "other-app", "prod", "KEY_A", "other", config.UpsertOptions{})

	results, err := svc.ListConfigs(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(results) != 1 || results[0].Key != "KEY_A" {
		t.Fatalf("got %+v", results)
	}
}

func TestListConfigs_ReturnsEmptyForUnknownScope(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	results, err := svc.ListConfigs(context.Background(), "unknown", "prod")
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %+v", results)
	}
}

func TestDecryptConfigValueOrOriginal_ReturnsPlaintextForLegacyRow(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)

	legacyEntry := &config.ConfigEntry{ID: uuid.New(), Key: "LEGACY", Value: "plaintext-value"}
	if got := svc.DecryptConfigValueOrOriginal(legacyEntry); got != "plaintext-value" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptConfigValueOrOriginal_DecryptsNormalRow(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "KEY_A", "secret-value", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	if got := svc.DecryptConfigValueOrOriginal(entry); got != "secret-value" {
		t.Fatalf("got %q", got)
	}
}

func TestDeleteConfig_RemovesEntryAndReturnsTrue(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
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

func TestDeleteConfig_ReturnsFalseForUnknownID(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	deleted, err := svc.DeleteConfig(context.Background(), uuid.New().String())
	if err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if deleted {
		t.Fatal("expected deleted = false")
	}
}

func TestDeleteConfig_ReturnsFalseForMalformedID(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	deleted, err := svc.DeleteConfig(context.Background(), "not-a-uuid")
	if err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if deleted {
		t.Fatal("expected deleted = false")
	}
}

func TestConfigHistory_UpsertRecordsCreateThenUpdateVersions(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v1.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig 1: %v", err)
	}
	if _, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v2.example.com", config.UpsertOptions{}); err != nil {
		t.Fatalf("UpsertConfig 2: %v", err)
	}

	history, err := svc.GetConfigHistory(ctx, entry.ID.String())
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if len(history) != 2 || history[0].Action != config.ActionUpdate || history[1].Action != config.ActionCreate {
		t.Fatalf("unexpected history: %+v", history)
	}
	if history[0].Version != 2 || history[1].Version != 1 {
		t.Fatalf("unexpected versions: %d, %d", history[0].Version, history[1].Version)
	}
	v0, _ := svc.DecryptVersionValue(&history[0])
	v1, _ := svc.DecryptVersionValue(&history[1])
	if v0 != "https://v2.example.com" || v1 != "https://v1.example.com" {
		t.Fatalf("unexpected decrypted values: %q, %q", v0, v1)
	}
}

func TestConfigHistory_UpsertRecordsChangedBy(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v1.example.com", config.UpsertOptions{ChangedBy: "admin@example.com"})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}

	history, err := svc.GetConfigHistory(ctx, entry.ID.String())
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if history[0].ChangedBy == nil || *history[0].ChangedBy != "admin@example.com" {
		t.Fatalf("changed_by = %v", history[0].ChangedBy)
	}
}

func TestConfigHistory_DeletingConfigDeletesItsHistory(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v1.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig 1: %v", err)
	}
	svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v2.example.com", config.UpsertOptions{})

	svc.DeleteConfig(ctx, entry.ID.String())

	history, err := svc.GetConfigHistory(ctx, entry.ID.String())
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected empty history, got %+v", history)
	}
}

func TestConfigHistory_RecreatingDeletedKeyStartsFreshHistory(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	entry, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v1.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	svc.DeleteConfig(ctx, entry.ID.String())
	recreated, err := svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://v2.example.com", config.UpsertOptions{})
	if err != nil {
		t.Fatalf("UpsertConfig recreate: %v", err)
	}

	history, err := svc.GetConfigHistory(ctx, recreated.ID.String())
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if len(history) != 1 || history[0].Action != config.ActionCreate || history[0].Version != 1 {
		t.Fatalf("unexpected history: %+v", history)
	}
}

func TestRollbackConfig_RestoresPriorValueAsNewVersion(t *testing.T) {
	svc, _, _, configs := newTestConfigService(t)
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
	if versions[0].ChangedBy == nil || *versions[0].ChangedBy != "admin@example.com" {
		t.Fatalf("changed_by = %v", versions[0].ChangedBy)
	}
}

func TestRollbackConfig_ReturnsNilForUnknownVersion(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
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

func TestRollbackConfig_ReturnsNilForUnknownConfigID(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	got, err := svc.RollbackConfig(context.Background(), uuid.New().String(), 1, "")
	if err != nil {
		t.Fatalf("RollbackConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestGetConfigHistory_ReturnsEmptyForUnknownID(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	history, err := svc.GetConfigHistory(context.Background(), uuid.New().String())
	if err != nil {
		t.Fatalf("GetConfigHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("got %+v", history)
	}
}

func TestGetConfigForClient_ReencryptsWithClientKey(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "API_URL", "https://api.example.com", config.UpsertOptions{})
	clientKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	result, err := svc.GetConfigForClient(ctx, "payments", "prod", "API_URL", clientKey)
	if err != nil {
		t.Fatalf("GetConfigForClient: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Value == "https://api.example.com" {
		t.Fatal("expected re-encrypted value")
	}
	decrypted, err := crypto.DecryptValue(result.Value, clientKey)
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}
	if decrypted != "https://api.example.com" {
		t.Fatalf("decrypted = %q", decrypted)
	}
}

func TestGetConfigForClient_ReturnsNilWhenMissing(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	clientKey, _ := crypto.GenerateKey()
	got, err := svc.GetConfigForClient(context.Background(), "payments", "prod", "MISSING", clientKey)
	if err != nil {
		t.Fatalf("GetConfigForClient: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestListConfigsForClient_EncryptsEachEntryForTheClient(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "KEY_A", "a", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "payments", "prod", "KEY_B", "b", config.UpsertOptions{})
	clientKey, _ := crypto.GenerateKey()

	results, err := svc.ListConfigsForClient(ctx, "payments", "prod", clientKey)
	if err != nil {
		t.Fatalf("ListConfigsForClient: %v", err)
	}
	keys := map[string]bool{}
	for _, r := range results {
		keys[r.Key] = true
	}
	if !keys["KEY_A"] || !keys["KEY_B"] || len(keys) != 2 {
		t.Fatalf("unexpected keys: %+v", keys)
	}
}

func TestListConfigsForClient_SkipsEntriesNotDecryptableUnderMasterKey(t *testing.T) {
	svc, apps, envs, configs := newTestConfigService(t)
	ctx := context.Background()
	app := &config.Application{Name: "payments"}
	if err := apps.Create(ctx, app); err != nil {
		t.Fatalf("create app: %v", err)
	}
	env := &config.Environment{ApplicationID: app.ID, Name: "prod"}
	if err := envs.Create(ctx, env); err != nil {
		t.Fatalf("create env: %v", err)
	}
	if err := configs.Update(ctx, &config.ConfigEntry{ID: uuid.New(), ApplicationID: app.ID, EnvironmentID: env.ID, Key: "GOOD", Value: "not-valid-fernet-data"}); err != nil {
		t.Fatalf("seed undecryptable entry: %v", err)
	}
	clientKey, _ := crypto.GenerateKey()

	results, err := svc.ListConfigsForClient(ctx, "payments", "prod", clientKey)
	if err != nil {
		t.Fatalf("ListConfigsForClient: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %+v", results)
	}
}

func TestListConfigsForClient_ServesFromCacheOnSecondCall(t *testing.T) {
	svc, _, _, configs := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "KEY_A", "a", config.UpsertOptions{})
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
	svc, _, _, _ := newTestConfigService(t)
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

func TestListServices_ReturnsDistinctSortedNames(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "zeta-app", "prod", "K", "v", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "alpha-app", "prod", "K", "v", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "zeta-app", "prod", "K2", "v", config.UpsertOptions{})

	services, err := svc.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}

	alphaIdx, zetaIdx, zetaCount := -1, -1, 0
	for i, s := range services {
		if s == "alpha-app" {
			alphaIdx = i
		}
		if s == "zeta-app" {
			zetaIdx = i
			zetaCount++
		}
	}
	if alphaIdx == -1 || zetaIdx == -1 || alphaIdx >= zetaIdx {
		t.Fatalf("unexpected order: %+v", services)
	}
	if zetaCount != 1 {
		t.Fatalf("expected zeta-app once, got %d times in %+v", zetaCount, services)
	}
}

func TestListEnvironments_ScopedToApplication(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	ctx := context.Background()
	svc.UpsertConfig(ctx, "payments", "prod", "K", "v", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "payments", "staging", "K", "v", config.UpsertOptions{})
	svc.UpsertConfig(ctx, "other-app", "dev", "K", "v", config.UpsertOptions{})

	envs, err := svc.ListEnvironments(ctx, "payments")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 2 || envs[0] != "prod" || envs[1] != "staging" {
		t.Fatalf("got %+v", envs)
	}
}

func TestListEnvironments_ReturnsEmptyForUnknownApplication(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	envs, err := svc.ListEnvironments(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 0 {
		t.Fatalf("got %+v", envs)
	}
}

func TestUpdateConfigEntry_ReturnsNilForUnknownID(t *testing.T) {
	svc, _, _, _ := newTestConfigService(t)
	got, err := svc.UpdateConfigEntry(context.Background(), uuid.New(), config.UpdateConfigEntryInput{})
	if err != nil {
		t.Fatalf("UpdateConfigEntry: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v", got)
	}
}
