// Package activity writes to the append-only activity log, mirroring
// common/activity_logger.py. Audit writes must never take down the request
// they're describing, so failures are logged, not returned/raised.
package activity

import (
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

func (l *Logger) log(typ, resource, resourceID, resourceName, userEmail string, ctx *Context, details any) {
	if ctx != nil && userEmail == "" {
		userEmail = ctx.UserEmail
	}

	var ip *string
	if ctx != nil && ctx.IPAddress != "" {
		ip = &ctx.IPAddress
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

	if err := l.db.Create(&a).Error; err != nil {
		slog.Error("failed to record activity", "type", typ, "resource", resource, "resource_id", resourceID, "user", userEmail, "err", err)
	}
}

func (l *Logger) LogCreate(resource, resourceID, resourceName string, ctx *Context, details any) {
	l.log(config.ActivityTypeCreate, resource, resourceID, resourceName, "", ctx, details)
}

func (l *Logger) LogUpdate(resource, resourceID, resourceName string, ctx *Context, details any) {
	l.log(config.ActivityTypeUpdate, resource, resourceID, resourceName, "", ctx, details)
}

func (l *Logger) LogDelete(resource, resourceID, resourceName string, ctx *Context, details any) {
	l.log(config.ActivityTypeDelete, resource, resourceID, resourceName, "", ctx, details)
}

func (l *Logger) LogToggle(resource, resourceID, resourceName string, ctx *Context, details any) {
	l.log(config.ActivityTypeToggle, resource, resourceID, resourceName, "", ctx, details)
}

func (l *Logger) LogLogin(userEmail string, ctx *Context, details any) {
	l.log(config.ActivityTypeLogin, "user", userEmail, userEmail, userEmail, ctx, details)
}

func (l *Logger) LogLogout(userEmail string, ctx *Context) {
	l.log(config.ActivityTypeLogout, "user", userEmail, userEmail, userEmail, ctx, nil)
}

// LogLoginFailed records a failed authentication attempt (bad password, unknown user, etc.).
func (l *Logger) LogLoginFailed(identifier string, ctx *Context, details any) {
	l.log(config.ActivityTypeLoginFailed, "user", identifier, identifier, identifier, ctx, details)
}

// LogAuthFailed records a failed non-user authentication attempt (e.g. an invalid S2S API key).
func (l *Logger) LogAuthFailed(resource, identifier string, ctx *Context, details any) {
	l.log(config.ActivityTypeAuthFailed, resource, identifier, identifier, "", ctx, details)
}
