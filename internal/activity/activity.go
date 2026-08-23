// Package activity writes to the append-only activity log, mirroring
// common/activity_logger.py. Audit writes must never take down the request
// they're describing, so failures are logged, not returned/raised.
package activity

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/config"
)

type Logger struct {
	db *gorm.DB
}

func NewLogger(db *gorm.DB) *Logger {
	return &Logger{db: db}
}

// Context carries the request-derived fields (client IP, current user email)
// that get attached to every activity row.
type Context struct {
	IPAddress string
	UserEmail string
}

func (l *Logger) log(ctx context.Context, typ, resource, resourceID, resourceName, userEmail string, actCtx *Context, details any) {
	if actCtx != nil && userEmail == "" {
		userEmail = actCtx.UserEmail
	}

	var ip *string
	if actCtx != nil && actCtx.IPAddress != "" {
		ip = &actCtx.IPAddress
	}

	var detailsStr *string
	if details != nil {
		b, err := json.Marshal(details)
		if err != nil {
			slog.Warn("failed to serialize activity details", "resource", resource, "resource_id", resourceID, "err", err)
		} else {
			s := string(b)
			detailsStr = &s
		}
	}

	var namePtr *string
	if resourceName != "" {
		namePtr = &resourceName
	}
	var emailPtr *string
	if userEmail != "" {
		emailPtr = &userEmail
	}

	a := config.Activity{
		ID:           uuid.New(),
		Type:         typ,
		Resource:     resource,
		ResourceID:   resourceID,
		ResourceName: namePtr,
		UserEmail:    emailPtr,
		Details:      detailsStr,
		IPAddress:    ip,
	}

	if err := l.db.WithContext(ctx).Create(&a).Error; err != nil {
		slog.Error("failed to record activity", "type", typ, "resource", resource, "resource_id", resourceID, "user", userEmail, "err", err)
	}
}

func (l *Logger) LogCreate(ctx context.Context, resource, resourceID, resourceName string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeCreate, resource, resourceID, resourceName, "", actCtx, details)
}

func (l *Logger) LogUpdate(ctx context.Context, resource, resourceID, resourceName string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeUpdate, resource, resourceID, resourceName, "", actCtx, details)
}

func (l *Logger) LogDelete(ctx context.Context, resource, resourceID, resourceName string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeDelete, resource, resourceID, resourceName, "", actCtx, details)
}

func (l *Logger) LogToggle(ctx context.Context, resource, resourceID, resourceName string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeToggle, resource, resourceID, resourceName, "", actCtx, details)
}

func (l *Logger) LogLogin(ctx context.Context, userEmail string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeLogin, "user", userEmail, userEmail, userEmail, actCtx, details)
}

func (l *Logger) LogLogout(ctx context.Context, userEmail string, actCtx *Context) {
	l.log(ctx, config.ActivityTypeLogout, "user", userEmail, userEmail, userEmail, actCtx, nil)
}

// LogLoginFailed records a failed authentication attempt (bad password, unknown user, etc.).
func (l *Logger) LogLoginFailed(ctx context.Context, identifier string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeLoginFailed, "user", identifier, identifier, identifier, actCtx, details)
}

// LogAuthFailed records a failed non-user authentication attempt (e.g. an invalid S2S API key).
func (l *Logger) LogAuthFailed(ctx context.Context, resource, identifier string, actCtx *Context, details any) {
	l.log(ctx, config.ActivityTypeAuthFailed, resource, identifier, identifier, "", actCtx, details)
}
