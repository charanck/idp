// Package db wires up the GORM/Postgres connection and applies goose
// migrations. GORM is used purely as a query layer here - goose, not GORM's
// AutoMigrate, owns the schema.
package db

import (
	"database/sql"
	"embed"
	"fmt"

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

// Migrate applies all pending goose migrations embedded in migrations/.
func Migrate(sqlDB *sql.DB) error {
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		return fmt.Errorf("run goose migrations: %w", err)
	}
	return nil
}
