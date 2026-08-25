package main

import (
	"fmt"

	"gorm.io/gorm"
	gormtracing "gorm.io/plugin/opentelemetry/tracing"

	"controlplane/internal/appconfig"
	"controlplane/internal/db"
	"controlplane/internal/observability"
)

// newDatabase opens the Postgres connection, installs OTEL tracing when
// enabled, and runs pending migrations.
func newDatabase(cfg *appconfig.Config) (*gorm.DB, error) {
	gdb, err := db.Open(cfg.DSN(), cfg.Debug)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if observability.Enabled() {
		if err := gdb.Use(gormtracing.NewPlugin(gormtracing.WithDBSystem("postgresql"))); err != nil {
			return nil, fmt.Errorf("install gorm otel plugin: %w", err)
		}
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return gdb, nil
}
