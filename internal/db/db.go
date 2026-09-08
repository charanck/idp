// Package db wires up the GORM/Postgres connection and applies goose
// migrations. GORM is used purely as a query layer here - goose, not GORM's
// AutoMigrate, owns the schema.
package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"

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
// against a database whose schema was created by a previous, non-goose
// migration tool - "migrating" onto that DB is then just goose recording its
// own version-tracking bookkeeping, since there is no row data to copy
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

	// goose only tracks which version numbers have been applied
	// (goose_db_version), never the content of the migration files
	// themselves - so silently editing a migration file after it has already
	// been applied to a given database (e.g. mid-development, before it's
	// committed) produces no error from goose, just a database whose real
	// schema has quietly diverged from what's on disk. verifyMigrationChecksums
	// closes that gap: it's what actually caught the "branding.accent_color
	// column does not exist" incident this guards against.
	if err := verifyMigrationChecksums(ctx, conn); err != nil {
		return err
	}
	return nil
}

// verifyMigrationChecksums detects drift between the migration files
// embedded in this binary and what was actually applied to the database in
// the past: for every migration version goose considers applied, it compares
// the file's current sha256 against the checksum recorded the first time
// that version was seen. A mismatch means the file was edited after being
// applied - e.g. a column was added to an already-run CREATE TABLE - and
// fails startup with a clear, specific error rather than continuing against
// a schema that no longer matches the code (see Migrate's doc comment).
//
// The tracking table is bootstrapped lazily: the first time a given version
// is observed (including every already-applied version, the first time this
// check itself is deployed) its current checksum is simply recorded, since
// there is nothing yet to compare it against.
func verifyMigrationChecksums(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS goose_migration_checksums (
			version     bigint PRIMARY KEY,
			filename    text NOT NULL,
			checksum    text NOT NULL,
			recorded_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create goose_migration_checksums table: %w", err)
	}

	var currentVersion int64
	if err := conn.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`,
	).Scan(&currentVersion); err != nil {
		return fmt.Errorf("determine current goose version: %w", err)
	}

	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	var drifted []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		versionStr, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}
		version, err := strconv.ParseInt(versionStr, 10, 64)
		if err != nil || version > currentVersion {
			continue
		}

		content, err := fs.ReadFile(embeddedMigrations, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(content)
		checksum := hex.EncodeToString(sum[:])

		var recorded string
		err = conn.QueryRowContext(ctx,
			`SELECT checksum FROM goose_migration_checksums WHERE version = $1`, version,
		).Scan(&recorded)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := conn.ExecContext(ctx,
				`INSERT INTO goose_migration_checksums (version, filename, checksum) VALUES ($1, $2, $3)`,
				version, name, checksum,
			); err != nil {
				return fmt.Errorf("record checksum for %s: %w", name, err)
			}
		case err != nil:
			return fmt.Errorf("look up checksum for %s: %w", name, err)
		case recorded != checksum:
			drifted = append(drifted, name)
		}
	}

	if len(drifted) > 0 {
		sort.Strings(drifted)
		return fmt.Errorf(
			"schema drift detected: already-applied migration(s) %s were modified on disk after being applied - "+
				"add a new migration instead of editing one that's already been run, or if this environment's "+
				"database is disposable, reset it and let migrations re-apply from scratch",
			strings.Join(drifted, ", "),
		)
	}
	return nil
}

// hasPreExistingSchema reports whether the target database already has
// domain tables (e.g. from a previous, non-goose migration tool) before
// goose has run, purely for the startup log line above - goose's own IF NOT
// EXISTS baseline behaves correctly either way.
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
