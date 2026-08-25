package notification

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// NotificationService creates and reads notifications, and enqueues them for
// asynchronous delivery.
type NotificationService struct {
	repo     NotificationRepository
	enqueuer Enqueuer
}

// Enqueuer schedules a notification for delivery. Satisfied by *TaskEnqueuer.
type Enqueuer interface {
	EnqueueSend(ctx context.Context, notificationID uuid.UUID) error
}

func NewNotificationService(repo NotificationRepository, enqueuer Enqueuer) *NotificationService {
	return &NotificationService{repo: repo, enqueuer: enqueuer}
}

// CreateNotificationInput bundles CreateNotification's parameters.
type CreateNotificationInput struct {
	Channel        string
	Recipient      datatypes.JSON
	Content        datatypes.JSON
	IdempotencyKey string
}

// CreateNotification inserts a queued notification and enqueues it for
// delivery. If IdempotencyKey is set and already exists: a still-queued
// match is best-effort re-enqueued and returned unchanged; a match past
// "queued" is returned as-is (no-op). A unique-violation race on insert is
// treated the same as "found" by re-reading the existing row.
func (s *NotificationService) CreateNotification(ctx context.Context, in CreateNotificationInput) (*Notification, error) {
	if in.IdempotencyKey != "" {
		existing, err := s.repo.FindByIdempotencyKey(ctx, in.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.Status == StatusQueued {
				if err := s.enqueuer.EnqueueSend(ctx, existing.ID); err != nil {
					slog.Error("re-enqueue idempotent notification failed", "id", existing.ID, "err", err)
				}
			}
			return existing, nil
		}
	}

	n := &Notification{
		Channel:   in.Channel,
		Recipient: in.Recipient,
		Content:   in.Content,
		Status:    StatusQueued,
	}
	if in.IdempotencyKey != "" {
		key := in.IdempotencyKey
		n.IdempotencyKey = &key
	}

	if err := s.repo.Create(ctx, n); err != nil {
		return nil, err
	}

	if err := s.enqueuer.EnqueueSend(ctx, n.ID); err != nil {
		return nil, err
	}

	slog.Info("created notification", "id", n.ID, "channel", n.Channel)
	return n, nil
}

// GetNotification returns a notification by ID, or nil if not found.
func (s *NotificationService) GetNotification(ctx context.Context, id uuid.UUID) (*Notification, error) {
	return s.repo.FindByID(ctx, id)
}

// ListNotificationsFilter filters ListNotifications.
type ListNotificationsFilter struct {
	Channel string
	Status  string
}

// ListNotifications lists notifications, newest first.
func (s *NotificationService) ListNotifications(ctx context.Context, filter ListNotificationsFilter) ([]Notification, error) {
	return s.repo.List(ctx, filter)
}

// ConsumeUnreadInAppForUser lists a user's unread InApp notifications, newest
// first, and marks them read in the same transaction - e.g. so a client
// catching up on missed notifications only ever sees each one once. Scoped
// to ChannelInApp only: InApp is the sole channel with a pull-based inbox,
// unlike email/sms/whatsapp (fire-and-forget delivery) or SSE (never
// persisted at all). The user is matched by the "user_id" field in the
// jsonb Recipient blob, the same external identifier the notification
// session token uses.
func (s *NotificationService) ConsumeUnreadInAppForUser(ctx context.Context, userID string) ([]Notification, error) {
	return s.repo.ConsumeUnreadInApp(ctx, userID)
}

// markProcessing/markSent/markRetrying/markFailed are worker-facing status
// transitions, unexported since only the worker's send path drives them.

func (s *NotificationService) markProcessing(ctx context.Context, id uuid.UUID) error {
	return s.repo.MarkProcessing(ctx, id)
}

func (s *NotificationService) markSent(ctx context.Context, id uuid.UUID, provider, providerMessageID string) error {
	return s.repo.MarkSent(ctx, id, provider, providerMessageID)
}

func (s *NotificationService) markRetrying(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return s.repo.MarkRetrying(ctx, id, attempt, sendErr)
}

func (s *NotificationService) markFailed(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return s.repo.MarkFailed(ctx, id, attempt, sendErr)
}
