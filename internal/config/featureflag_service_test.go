package config_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/cache"
	"controlplane/internal/config"
	"controlplane/internal/testutil"
)

func setupFlagService(t *testing.T) (*gorm.DB, *config.FeatureFlagService) {
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
	svc := config.NewFeatureFlagService(config.NewFeatureFlagRepository(gdb), config.NewApplicationRepository(gdb), config.NewEnvironmentRepository(gdb), c, 5*time.Minute)
	return gdb, svc
}

func seedPaymentsWithEnvs(t *testing.T, gdb *gorm.DB, envs ...string) *config.Application {
	t.Helper()
	app := &config.Application{Name: "payments"}
	if err := gdb.Create(app).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	for _, name := range envs {
		if err := gdb.Create(&config.Environment{ApplicationID: app.ID, Name: name}).Error; err != nil {
			t.Fatalf("create environment %s: %v", name, err)
		}
	}
	return app
}

func TestCreateFlag_CreatesAcrossAllEnvironmentsByDefault(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod", "staging")
	ctx := context.Background()

	flags, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{
		Description: "desc", IsEnabled: true, CreateAllEnvironments: true,
	})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if len(flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(flags))
	}
	names := map[string]bool{}
	for _, f := range flags {
		var env config.Environment
		gdb.First(&env, "id = ?", f.EnvironmentID)
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
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod", "staging")
	ctx := context.Background()

	flags, err := svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{
		Environment: "prod", CreateAllEnvironments: false,
	})
	if err != nil {
		t.Fatalf("CreateFlag: %v", err)
	}
	if len(flags) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(flags))
	}
	var env config.Environment
	gdb.First(&env, "id = ?", flags[0].EnvironmentID)
	if env.Name != "prod" {
		t.Fatalf("environment = %q", env.Name)
	}
}

func TestCreateFlag_RaisesWhenSingleEnvironmentRequestedWithoutName(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")

	_, err := svc.CreateFlag(context.Background(), "payments", "new-checkout", config.CreateFlagOptions{CreateAllEnvironments: false})
	if !errors.Is(err, config.ErrEnvironmentRequired) {
		t.Fatalf("err = %v, want ErrEnvironmentRequired", err)
	}
}

func TestCreateFlag_RaisesWhenNoEnvironmentsExistForApplication(t *testing.T) {
	gdb, svc := setupFlagService(t)
	if err := gdb.Create(&config.Application{Name: "empty-app"}).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}

	_, err := svc.CreateFlag(context.Background(), "empty-app", "new-flag", config.CreateFlagOptions{CreateAllEnvironments: true})
	if !errors.Is(err, config.ErrNoEnvironmentsFound) {
		t.Fatalf("err = %v, want ErrNoEnvironmentsFound", err)
	}
}

func TestCreateFlag_RaisesWhenApplicationUnknown(t *testing.T) {
	_, svc := setupFlagService(t)
	_, err := svc.CreateFlag(context.Background(), "unknown-app", "new-flag", config.CreateFlagOptions{CreateAllEnvironments: true})
	if !errors.Is(err, config.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
}

func TestCreateFlag_RecreatingFlagUndoesPreviousSoftDelete(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")
	ctx := context.Background()

	svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{CreateAllEnvironments: true})

	now := time.Now().UTC()
	if err := gdb.Model(&config.FeatureFlag{}).Where("name = ?", "new-checkout").Update("deleted_at", now).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	svc.CreateFlag(ctx, "payments", "new-checkout", config.CreateFlagOptions{CreateAllEnvironments: true})

	var flag config.FeatureFlag
	gdb.Where("name = ?", "new-checkout").First(&flag)
	if flag.DeletedAt != nil {
		t.Fatalf("expected deleted_at to be cleared, got %v", flag.DeletedAt)
	}
}

func TestGetFlag_ReturnsNilWhenMissing(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")

	got, err := svc.GetFlag(context.Background(), "payments", "prod", "missing-flag")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestGetFlag_ReturnsNilWhenSoftDeleted(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")
	ctx := context.Background()
	svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod"})

	now := time.Now().UTC()
	gdb.Model(&config.FeatureFlag{}).Where("name = ?", "my-flag").Update("deleted_at", now)

	got, err := svc.GetFlag(ctx, "payments", "prod", "my-flag")
	if err != nil {
		t.Fatalf("GetFlag: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestListFlags_ExcludesSoftDeleted(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")
	ctx := context.Background()
	svc.CreateFlag(ctx, "payments", "keep-me", config.CreateFlagOptions{Environment: "prod"})
	svc.CreateFlag(ctx, "payments", "delete-me", config.CreateFlagOptions{Environment: "prod"})
	gdb.Model(&config.FeatureFlag{}).Where("name = ?", "delete-me").Update("deleted_at", time.Now().UTC())

	flags, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(flags) != 1 || flags[0].Name != "keep-me" {
		t.Fatalf("got %+v", flags)
	}
}

func TestListFlags_ReturnsEmptyForUnknownScope(t *testing.T) {
	_, svc := setupFlagService(t)
	flags, err := svc.ListFlags(context.Background(), "unknown", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if len(flags) != 0 {
		t.Fatalf("got %+v", flags)
	}
}

func TestToggleFlag_FlipsState(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")
	ctx := context.Background()
	svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod", IsEnabled: false})

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
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")

	got, err := svc.ToggleFlag(context.Background(), "payments", "prod", "missing-flag")
	if err != nil {
		t.Fatalf("ToggleFlag: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil")
	}
}

func TestToggleFlag_InvalidatesListCache(t *testing.T) {
	gdb, svc := setupFlagService(t)
	seedPaymentsWithEnvs(t, gdb, "prod")
	ctx := context.Background()
	svc.CreateFlag(ctx, "payments", "my-flag", config.CreateFlagOptions{Environment: "prod", IsEnabled: false})

	cachedBefore, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if cachedBefore[0].IsEnabled {
		t.Fatal("expected disabled before toggle")
	}

	svc.ToggleFlag(ctx, "payments", "prod", "my-flag")

	cachedAfter, err := svc.ListFlags(ctx, "payments", "prod")
	if err != nil {
		t.Fatalf("ListFlags: %v", err)
	}
	if !cachedAfter[0].IsEnabled {
		t.Fatal("expected enabled after toggle (cache should be invalidated)")
	}
}
