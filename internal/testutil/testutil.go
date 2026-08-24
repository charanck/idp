// Package testutil provides shared test helpers for spinning up a real
// Postgres connection (migrated) for integration tests, since goose/GORM
// target Postgres-specific types that sqlite can't stand in for.
package testutil

import (
	"os"
	"sync"
	"testing"

	"gorm.io/gorm"

	"controlplane/internal/db"
)

var (
	once   sync.Once
	sqlErr error
)

// TestDSN returns the DSN for the ephemeral test Postgres instance, skipping
// the test if CP_TEST_DATABASE_URL isn't set (e.g. in CI without Docker).
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CP_TEST_DATABASE_URL not set; skipping integration test")
	}
	return dsn
}

// OpenDB connects to the test Postgres instance and ensures migrations have
// been applied (once per test binary run), returning a fresh *gorm.DB.
func OpenDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testDSN(t)

	gdb, err := db.Open(dsn, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	once.Do(func() {
		sqlDB, e := gdb.DB()
		if e != nil {
			sqlErr = e
			return
		}
		sqlErr = db.Migrate(sqlDB)
	})
	if sqlErr != nil {
		t.Fatalf("db.Migrate: %v", sqlErr)
	}

	return gdb
}

// TruncateAll clears all domain tables between tests so each test starts
// from a clean slate without needing a fresh container per test.
func TruncateAll(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	tables := []string{
		"notifications", "notification_provider_settings",
		"oauth_user_tokens", "oauth_providers",
		"config_entry_versions", "config_entries", "feature_flags",
		"environments", "applications",
		"activities", "service_clients", "users",
	}
	for _, table := range tables {
		if err := gdb.Exec("TRUNCATE TABLE " + table + " CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}
