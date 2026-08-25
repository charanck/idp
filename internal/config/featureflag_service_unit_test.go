package config_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"controlplane/internal/config"
)

func newUnitFlagService(t *testing.T) (*config.FeatureFlagService, *fakeApplicationRepository, *fakeEnvironmentRepository, *fakeFeatureFlagRepository) {
	t.Helper()
	apps := newFakeApplicationRepository()
	envs := newFakeEnvironmentRepository(apps)
	flags := newFakeFeatureFlagRepository(apps, envs)
	svc := config.NewFeatureFlagService(flags, apps, envs, newFakeCache(), time.Minute)
	return svc, apps, envs, flags
}

func seedUnitApp(t *testing.T, apps *fakeApplicationRepository, envs *fakeEnvironmentRepository, name string, envNames ...string) *config.Application {
	t.Helper()
	ctx := context.Background()
	app := &config.Application{Name: name}
	if err := apps.Create(ctx, app); err != nil {
		t.Fatalf("create application: %v", err)
	}
	for _, envName := range envNames {
		if err := envs.Create(ctx, &config.Environment{ApplicationID: app.ID, Name: envName}); err != nil {
			t.Fatalf("create environment %s: %v", envName, err)
		}
	}
	return app
}

func TestCreateFlag_UndeletesSoftDeletedFlagOnRecreate(t *testing.T) {
	svc, apps, envs, flags := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod")
	ctx := context.Background()

	created, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{CreateAllEnvironments: true})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(created))
	}

	now := time.Now()
	created[0].DeletedAt = &now
	if err := flags.Update(ctx, &created[0]); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	recreated, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{CreateAllEnvironments: true})
	if err != nil {
		t.Fatalf("CreateFlag (recreate): %v", err)
	}
	if len(recreated) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(recreated))
	}
	if recreated[0].DeletedAt != nil {
		t.Fatalf("expected deleted_at cleared, got %v", recreated[0].DeletedAt)
	}
}

func TestCreateFlag_CreatesAcrossAllEnvironments(t *testing.T) {
	svc, apps, envs, _ := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod", "staging")
	ctx := context.Background()

	created, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{IsEnabled: true, CreateAllEnvironments: true})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(created))
	}
}

func TestCreateFlag_RaisesWhenApplicationUnknown_FakeRepo(t *testing.T) {
	svc, _, _, _ := newUnitFlagService(t)
	_, err := svc.CreateFlag(context.Background(), "unknown-app", "flag", config.CreateFlagOptions{CreateAllEnvironments: true})
	if !errors.Is(err, config.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
}

func TestCreateFlag_RaisesWhenSingleEnvironmentRequestedWithoutName_FakeRepo(t *testing.T) {
	svc, apps, envs, _ := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod")

	_, err := svc.CreateFlag(context.Background(), "payments", "flag", config.CreateFlagOptions{CreateAllEnvironments: false})
	if !errors.Is(err, config.ErrEnvironmentRequired) {
		t.Fatalf("err = %v, want ErrEnvironmentRequired", err)
	}
}

func TestCreateFlag_RaisesWhenNoEnvironmentsExist(t *testing.T) {
	svc, apps, envs, _ := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "empty-app")

	_, err := svc.CreateFlag(context.Background(), "empty-app", "flag", config.CreateFlagOptions{CreateAllEnvironments: true})
	if !errors.Is(err, config.ErrNoEnvironmentsFound) {
		t.Fatalf("err = %v, want ErrNoEnvironmentsFound", err)
	}
}

func TestGetFlag_ReturnsNilWhenSoftDeleted_FakeRepo(t *testing.T) {
	svc, apps, envs, flags := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod")
	ctx := context.Background()

	created, err := svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod"})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	now := time.Now()
	created[0].DeletedAt = &now
	if err := flags.Update(ctx, &created[0]); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	got, err := svc.GetFlag(ctx, "payments", "prod", "my-flag")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestToggleFlag_FlipsState_FakeRepo(t *testing.T) {
	svc, apps, envs, _ := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod")
	ctx := context.Background()
	if _, err := svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod", IsEnabled: false}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	toggled, err := svc.ToggleFlag(ctx, "payments", "prod", "my-flag")
	if err != nil {
		t.Fatalf("ToggleFlag: %v", err)
	}
	if !toggled.IsEnabled {
		t.Fatal("expected enabled after first toggle")
	}
}

func TestListFlags_ServesFromCacheOnSecondCall(t *testing.T) {
	svc, apps, envs, flags := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod")
	ctx := context.Background()
	if _, err := svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod", IsEnabled: false}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	first, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags (1st): %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(first))
	}

	flags.listActiveByScopeCalls = 0
	second, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags (2nd): %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 flag from cache, got %d", len(second))
	}
	if flags.listActiveByScopeCalls != 0 {
		t.Fatalf("expected ListActiveByScope not to be called on a cache hit, got %d calls", flags.listActiveByScopeCalls)
	}
}

func TestToggleFlag_InvalidatesListCache_FakeRepo(t *testing.T) {
	svc, apps, envs, _ := newUnitFlagService(t)
	seedUnitApp(t, apps, envs, "payments", "prod")
	ctx := context.Background()
	if _, err := svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod", IsEnabled: false}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}

	cachedBefore, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if cachedBefore[0].IsEnabled {
		t.Fatal("expected disabled before toggle")
	}

	if _, err := svc.ToggleFlag(ctx, "payments", "prod", "my-flag"); err != nil {
		t.Fatalf("ToggleFlag: %v", err)
	}

	cachedAfter, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if !cachedAfter[0].IsEnabled {
		t.Fatal("expected enabled after toggle (cache should be invalidated)")
	}
}
