package http_test

import (
	"context"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/notification"
)

type fakeNotificationCreator struct {
	notification *notification.Notification
	err          error
}

func (f *fakeNotificationCreator) CreateNotification(ctx context.Context, input notification.CreateNotificationInput) (*notification.Notification, error) {
	return f.notification, f.err
}

type fakeNotificationLister struct {
	notifications []notification.Notification
	err           error
}

func (f *fakeNotificationLister) ListNotifications(ctx context.Context, filter notification.ListNotificationsFilter) ([]notification.Notification, error) {
	return f.notifications, f.err
}

type fakeNotificationGetter struct {
	notification *notification.Notification
	err          error
}

func (f *fakeNotificationGetter) GetNotification(ctx context.Context, id uuid.UUID) (*notification.Notification, error) {
	return f.notification, f.err
}

type fakeSessionIssuer struct {
	token string
	err   error
}

func (f *fakeSessionIssuer) Issue(userID string) (string, error) {
	return f.token, f.err
}

type fakeSessionValidator struct {
	userID string
	err    error
}

func (f *fakeSessionValidator) Validate(token string) (string, error) {
	return f.userID, f.err
}

type fakeUnreadConsumer struct {
	notifications []notification.Notification
	err           error
}

func (f *fakeUnreadConsumer) ConsumeUnreadInAppForUser(ctx context.Context, userID string) ([]notification.Notification, error) {
	return f.notifications, f.err
}

type fakeNotificationAuthenticator struct {
	subject string
	err     error
}

func (f *fakeNotificationAuthenticator) Authenticate(ctx context.Context, apiKey string) (string, error) {
	return f.subject, f.err
}

type fakeNotificationRateLimiter struct {
	limited bool
	err     error
}

func (f *fakeNotificationRateLimiter) IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error) {
	return f.limited, f.err
}
