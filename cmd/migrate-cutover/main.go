// Command migrate-cutover is a standalone operator tool for validating a
// Django -> Go cutover before flipping traffic: it runs the exact same
// locked schema reconciliation main.go runs automatically on every server
// startup (see internal/db.Migrate), so an operator can do it ahead of time
// against the shared production Postgres database and confirm it's clean.
//
// Usage:
//
//	go run ./cmd/migrate-cutover              # reconcile now (same as server startup)
//	go run ./cmd/migrate-cutover --dry-run     # report status only, apply nothing
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"controlplane/internal/db"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "report pending migrations and schema status without applying anything")
	flag.Parse()

	gdb, err := db.Open(dsnFromEnv(), false)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		log.Fatalf("get sql.DB: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	preExisting, err := db.HasPreExistingSchema(ctx, sqlDB)
	if err != nil {
		log.Fatalf("detect pre-existing schema: %v", err)
	}
	if preExisting {
		slog.Info("found pre-existing database schema (e.g. Django-managed)")
	} else {
		slog.Info("no pre-existing schema found; this would be a fresh baseline")
	}

	if *dryRun {
		slog.Info("dry-run: reporting goose status, applying nothing")
		if err := db.Status(sqlDB); err != nil {
			log.Fatalf("goose status: %v", err)
		}
		return
	}

	if err := db.Migrate(sqlDB); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	slog.Info("migration/reconciliation complete")
}

// dsnFromEnv builds a Postgres DSN from the same DB_* env vars
// appconfig.Config.DSN uses, without requiring MASTER_ENCRYPTION_KEY (this
// tool only touches schema, never config/secret values, so it shouldn't need
// the encryption key just to run).
func dsnFromEnv() string {
	get := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		get("DB_HOST", "localhost"), get("DB_PORT", "5432"), get("DB_NAME", "idp"),
		get("DB_USER", "idp"), get("DB_PASSWORD", "idp"), get("DB_SSLMODE", "disable"),
	)
}
