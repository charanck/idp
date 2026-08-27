package config_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"controlplane/internal/config"
)

func newTestFlagService(t *testing.T) (*config.FeatureFlagService, *fakeApplicationRepository, *fakeEnvironmentRepository, *fakeFeatureFlagRepository) {
	t.Helper()
	apps := newFakeApplicationRepository()
	envs := newFakeEnvironmentRepository(apps)
	flags := newFakeFeatureFlagRepository(apps, envs)
	svc := config.NewFeatureFlagService(flags, apps, envs, newFakeCache(), time.Minute)
	return svc, apps, envs, flags
}

func seedFlagApp(t *testing.T, apps *fakeApplicationRepository, envs *fakeEnvironmentRepository, name string, envNames ...string) *config.Application {
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

func TestCreateFlag_CreatesAcrossAllEnvironments(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod", "staging")
	ctx := context.Background()

	created, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{
		Description: "desc", IsEnabled: true, CreateAllEnvironments: true,
	})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(created))
	}
	names := map[string]bool{}
	for _, f := range created {
		env, err := envs.FindByID(ctx, f.EnvironmentID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		names[env.Name] = true
		if !f.IsEnabled {
			t.Fatal("expected all flags enabled")
		}
	}
	if !names["prod"] || !names["staging"] {
		t.Fatalf("unexpected environments: %+v", names)
	}
}

func TestCreateFlag_CreatesForSingleEnvironmentWhenRequested(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod", "staging")
	ctx := context.Background()

	created, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{
		Environment: "prod", CreateAllEnvironments: false,
	})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(created))
	}
	env, err := envs.FindByID(ctx, created[0].EnvironmentID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if env.Name != "prod" {
		t.Fatalf("environment = %q", env.Name)
	}
}

func TestCreateFlag_RaisesWhenSingleEnvironmentRequestedWithoutName(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")

	_, err := svc.CreateFlag(context.Background(), "payments", "flag", config.CreateFlagOptions{CreateAllEnvironments: false})
	if !errors.Is(err, config.ErrEnvironmentRequired) {
		t.Fatalf("err = %v, want ErrEnvironmentRequired", err)
	}
}

func TestCreateFlag_RaisesWhenNoEnvironmentsExist(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "empty-app")

	_, err := svc.CreateFlag(context.Background(), "empty-app", "flag", config.CreateFlagOptions{CreateAllEnvironments: true})
	if !errors.Is(err, config.ErrNoEnvironmentsFound) {
		t.Fatalf("err = %v, want ErrNoEnvironmentsFound", err)
	}
}

func TestCreateFlag_RaisesWhenApplicationUnknown(t *testing.T) {
	svc, _, _, _ := newTestFlagService(t)
	_, err := svc.CreateFlag(context.Background(), "unknown-app", "flag", config.CreateFlagOptions{CreateAllEnvironments: true})
	if !errors.Is(err, config.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
}

func TestCreateFlag_UndeletesSoftDeletedFlagOnRecreate(t *testing.T) {
	svc, apps, envs, flags := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")
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

func TestGetFlag_ReturnsNilWhenMissing(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")

	got, err := svc.GetFlag(context.Background(), "payments", "prod", "missing-flag")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestGetFlag_ReturnsNilWhenSoftDeleted(t *testing.T) {
	svc, apps, envs, flags := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")
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

func TestListFlags_ExcludesSoftDeleted(t *testing.T) {
	svc, apps, envs, flags := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")
	ctx := context.Background()
	if _, err := svc.CreateFlag(ctx, "payments", "keep-me", config.CreateFlagOptions{Environment: "prod"}); err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	deleteMe, err := svc.CreateFlag(ctx, "payments", "delete-me", config.CreateFlagOptions{Environment: "prod"})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	now := time.Now()
	deleteMe[0].DeletedAt = &now
	if err := flags.Update(ctx, &deleteMe[0]); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	got, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(got) != 1 || got[0].Name != "keep-me" {
		t.Fatalf("got %+v", got)
	}
}

func TestListFlags_ReturnsEmptyForUnknownScope(t *testing.T) {
	svc, _, _, _ := newTestFlagService(t)
	got, err := svc.ListFlags(context.Background(), "unknown", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestListFlags_ServesFromCacheOnSecondCall(t *testing.T) {
	svc, apps, envs, flags := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")
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

func TestToggleFlag_FlipsState(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")
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

	toggledAgain, err := svc.ToggleFlag(ctx, "payments", "prod", "my-flag")
	if err != nil {
		t.Fatalf("ToggleFlag: %v", err)
	}
	if toggledAgain.IsEnabled {
		t.Fatal("expected disabled after second toggle")
	}
}

func TestToggleFlag_ReturnsNilForMissingFlag(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")

	got, err := svc.ToggleFlag(context.Background(), "payments", "prod", "missing-flag")
	if err != nil {
		t.Fatalf("ToggleFlag: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestToggleFlag_InvalidatesListCache(t *testing.T) {
	svc, apps, envs, _ := newTestFlagService(t)
	seedFlagApp(t, apps, envs, "payments", "prod")
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
