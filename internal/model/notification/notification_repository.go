package model

import (
	"context"

	"github.com/google/uuid"
)

// ListNotificationsFilter filters NotificationRepository.List.
type ListNotificationsFilter struct {
	Channel string
	Status  string
}

// NotificationRepository is the persistence seam for Notification rows.
type NotificationRepository interface {
	FindByIdempotencyKey(ctx context.Context, key string) (*Notification, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Notification, error)
	List(ctx context.Context, filter ListNotificationsFilter) ([]Notification, error)
	Create(ctx context.Context, n *Notification) error
	// ConsumeUnreadInApp atomically lists and marks-read a user's unread
	// InApp notifications, newest first - kept as one transactional
	// repository method (rather than List+Update called separately by the
	// service) so a client catching up on missed notifications is
	// guaranteed to see each one exactly once.
	ConsumeUnreadInApp(ctx context.Context, userID string) ([]Notification, error)
	MarkProcessing(ctx context.Context, id uuid.UUID) error
	MarkSent(ctx context.Context, id uuid.UUID, provider, providerMessageID string) error
	MarkRetrying(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error
	MarkFailed(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error
}
