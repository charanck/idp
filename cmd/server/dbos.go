package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	"controlplane/internal/appconfig"
	"controlplane/internal/observability"
)

// newDBOSContext creates and connects the server's single DBOS context,
// backed by the app's own Postgres database, isolated into its own schema
// (dbos.Config.DatabaseSchema defaults to "dbos") rather than one goose
// owns. It is bootstrapped unconditionally in main(), independent of any
// individual feature (e.g. notification.Enabled) - any DBOS workflow the
// server registers, present or future, shares this one context/lifecycle
// rather than each feature standing up its own.
//
// NewContext synchronously creates/migrates the dbos schema before
// returning, so callers can safely register workflows/queues against the
// returned Context immediately - no need to wait for dbos.Launch.
//
// Logger is set to slog.Default() rather than left to DBOS's own
// newly-constructed logger so DBOS's internal logging flows through the
// same OTEL log bridge (see internal/observability) as the rest of the
// app's slog output, once setupObservability has run.
func newDBOSContext(cfg *appconfig.Config) (dbos.Context, error) {
	ctx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:     observability.ServiceName,
		DatabaseURL: cfg.DSN(),
		Logger:      slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("create dbos context: %w", err)
	}
	return ctx, nil
}
