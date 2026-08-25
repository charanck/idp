package webui

import (
	"context"

	"controlplane/internal/activity"
)

// ActivityRecorder is the write side of the audit log, used by every
// handler that mutates a resource. Satisfied by *activity.Logger.
type ActivityRecorder interface {
	LogCreate(ctx context.Context, resource, resourceID, resourceName string, details any)
	LogUpdate(ctx context.Context, resource, resourceID, resourceName string, details any)
	LogDelete(ctx context.Context, resource, resourceID, resourceName string, details any)
	LogToggle(ctx context.Context, resource, resourceID, resourceName string, details any)
	LogLogin(ctx context.Context, userEmail string, details any)
	LogLogout(ctx context.Context, userEmail string)
	LogLoginFailed(ctx context.Context, identifier string, details any)
}

var _ ActivityRecorder = (*activity.Logger)(nil)
