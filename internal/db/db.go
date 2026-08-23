// Package db wires up the GORM/Postgres connection and applies goose
// migrations. GORM is used purely as a query layer here - goose, not GORM's
// AutoMigrate, owns the schema.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed all:migrations
var embeddedMigrations embed.FS

// Open connects to Postgres via GORM using an already-built DSN.
func Open(dsn string, verbose bool) (*gorm.DB, error) {
	logLevel := logger.Silent
	if verbose {
		logLevel = logger.Info
	}

	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	return gdb, nil
}

// migrationLockKey is a fixed, well-known Postgres advisory-lock key (not
// hashed from anything - it must be the same constant across every process
// and every deploy) guarding the migrate/reconcile step below, so that
// multiple Go replicas booting concurrently against the same database don't
// race on schema setup.
const migrationLockKey int64 = 0x636f6e74726f6c31 // "controlplane1" packed into 8 bytes

// Migrate reconciles the database schema on startup and is safe to run on
// every container restart: it applies any pending goose migrations embedded
// in migrations/, whose baseline migration uses CREATE TABLE IF NOT EXISTS
// so it also no-ops cleanly (rather than colliding with existing tables)
// against a database whose schema was created by Django's own migrations
// instead of goose - "migrating" onto that DB is then just goose recording
// its own version-tracking bookkeeping, since there is no row data to copy
// between the two: they share one physical schema (see 00001_baseline.sql).
// The whole step is wrapped in a session-level Postgres advisory lock so
// concurrent replicas serialize on it instead of racing.
func Migrate(sqlDB *sql.DB) error {
	ctx := context.Background()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration lock connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
			slog.Error("failed to release migration advisory lock", "err", err)
		}
	}()

	preExisting, err := hasPreExistingSchema(ctx, conn)
	if err != nil {
		return fmt.Errorf("detect pre-existing schema: %w", err)
	}
	if preExisting {
		slog.Info("found pre-existing database schema; reconciling goose version tracking against it rather than creating tables")
	} else {
		slog.Info("no pre-existing schema found; applying baseline migration")
	}

	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		return fmt.Errorf("run goose migrations: %w", err)
	}
	return nil
}

// hasPreExistingSchema reports whether the target database already has
// domain tables (e.g. from Django's own migrations) before goose has run,
// purely for the startup log line above - goose's own IF NOT EXISTS baseline
// behaves correctly either way.
func hasPreExistingSchema(ctx context.Context, conn *sql.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'users')`,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// HasPreExistingSchema is the sql.DB-level equivalent of hasPreExistingSchema,
// for callers (cmd/migrate-cutover) that only hold a pool, not a single conn.
func HasPreExistingSchema(ctx context.Context, sqlDB *sql.DB) (bool, error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	return hasPreExistingSchema(ctx, conn)
}

// Status prints the current goose migration status (applied vs. pending)
// without applying anything, for pre-cutover validation - see
// cmd/migrate-cutover's --dry-run flag.
func Status(sqlDB *sql.DB) error {
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	return goose.Status(sqlDB, "migrations")
}
