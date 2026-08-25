package main

import (
	"context"

	"controlplane/internal/observability"
)

// setupObservability starts OTEL (if configured) and returns its shutdown
// func, thinly wrapping observability.Setup so main() reads as a flat list
// of "build this subsystem" steps.
func setupObservability(ctx context.Context, version string) (observability.Shutdown, error) {
	return observability.Setup(ctx, version)
}
