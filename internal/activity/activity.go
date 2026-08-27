// Package activity writes to the append-only activity log, mirroring
// common/activity_logger.py. Audit writes must never take down the request
// they're describing, so failures are logged, not returned/raised.
package activity

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	model "controlplane/internal/model/activity"
)

type Logger struct {
	repo model.Repository
}

func NewLogger(repo model.Repository) *Logger {
	return &Logger{repo: repo}
}

// requestInfoKey is the context.Context key under which request-derived
// fields (client IP, current user email) are stashed, so callers only need
// to thread a single context.Context through to the logger.
type requestInfoKey struct{}

// RequestInfo carries the request-derived fields that get attached to every
// activity row.
type RequestInfo struct {
	IPAddress string
	UserEmail string
}

// WithRequestInfo returns a copy of ctx carrying info for the activity
// logger to pick up on any Log* call made with it.
func WithRequestInfo(ctx context.Context, info RequestInfo) context.Context {
	return context.WithValue(ctx, requestInfoKey{}, info)
}

func requestInfoFromContext(ctx context.Context) RequestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(RequestInfo)
	return info
}

func (l *Logger) log(ctx context.Context, typ, resource, resourceID, resourceName, userEmail string, details any) {
	info := requestInfoFromContext(ctx)
	if userEmail == "" {
		userEmail = info.UserEmail
	}

	var ip *string
	if info.IPAddress != "" {
		ip = &info.IPAddress
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

	a := model.Activity{
		ID:           uuid.New(),
		Type:         typ,
		Resource:     resource,
		ResourceID:   resourceID,
		ResourceName: namePtr,
		UserEmail:    emailPtr,
		Details:      detailsStr,
		IPAddress:    ip,
	}

	if err := l.repo.Create(ctx, &a); err != nil {
		slog.Error("failed to record activity", "type", typ, "resource", resource, "resource_id", resourceID, "user", userEmail, "err", err)
	}
}

func (l *Logger) LogCreate(ctx context.Context, resource, resourceID, resourceName string, details any) {
	l.log(ctx, model.ActivityTypeCreate, resource, resourceID, resourceName, "", details)
}

func (l *Logger) LogUpdate(ctx context.Context, resource, resourceID, resourceName string, details any) {
	l.log(ctx, model.ActivityTypeUpdate, resource, resourceID, resourceName, "", details)
}

func (l *Logger) LogDelete(ctx context.Context, resource, resourceID, resourceName string, details any) {
	l.log(ctx, model.ActivityTypeDelete, resource, resourceID, resourceName, "", details)
}

func (l *Logger) LogToggle(ctx context.Context, resource, resourceID, resourceName string, details any) {
	l.log(ctx, model.ActivityTypeToggle, resource, resourceID, resourceName, "", details)
}

func (l *Logger) LogLogin(ctx context.Context, userEmail string, details any) {
	l.log(ctx, model.ActivityTypeLogin, "user", userEmail, userEmail, userEmail, details)
}

func (l *Logger) LogLogout(ctx context.Context, userEmail string) {
	l.log(ctx, model.ActivityTypeLogout, "user", userEmail, userEmail, userEmail, nil)
}

// LogLoginFailed records a failed authentication attempt (bad password, unknown user, etc.).
func (l *Logger) LogLoginFailed(ctx context.Context, identifier string, details any) {
	l.log(ctx, model.ActivityTypeLoginFailed, "user", identifier, identifier, identifier, details)
}

// LogAuthFailed records a failed non-user authentication attempt (e.g. an invalid S2S API key).
func (l *Logger) LogAuthFailed(ctx context.Context, resource, identifier string, details any) {
	l.log(ctx, model.ActivityTypeAuthFailed, resource, identifier, identifier, "", details)
}

// List lists activity rows matching filter, newest first.
func (l *Logger) List(ctx context.Context, filter model.ListFilter) ([]model.Activity, error) {
	return l.repo.List(ctx, filter)
}

// DistinctResources lists every distinct resource value ever logged, ordered alphabetically.
func (l *Logger) DistinctResources(ctx context.Context) ([]string, error) {
	return l.repo.DistinctResources(ctx)
}

// DistinctTypes lists every distinct activity type value ever logged, ordered alphabetically.
func (l *Logger) DistinctTypes(ctx context.Context) ([]string, error) {
	return l.repo.DistinctTypes(ctx)
}
