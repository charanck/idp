package notification

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// NotificationService creates and reads notifications, and enqueues them for
// asynchronous delivery.
type NotificationService struct {
	db       *gorm.DB
	enqueuer Enqueuer
}

// Enqueuer schedules a notification for delivery. Satisfied by *TaskEnqueuer.
type Enqueuer interface {
	EnqueueSend(ctx context.Context, notificationID uuid.UUID) error
}

func NewNotificationService(db *gorm.DB, enqueuer Enqueuer) *NotificationService {
	return &NotificationService{db: db, enqueuer: enqueuer}
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
		existing, err := s.getByIdempotencyKey(ctx, in.IdempotencyKey)
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

	if err := s.db.WithContext(ctx).Create(n).Error; err != nil {
		return nil, err
	}

	if err := s.enqueuer.EnqueueSend(ctx, n.ID); err != nil {
		return nil, err
	}

	slog.Info("created notification", "id", n.ID, "channel", n.Channel)
	return n, nil
}

func (s *NotificationService) getByIdempotencyKey(ctx context.Context, key string) (*Notification, error) {
	var n Notification
	err := s.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// GetNotification returns a notification by ID, or nil if not found.
func (s *NotificationService) GetNotification(ctx context.Context, id uuid.UUID) (*Notification, error) {
	var n Notification
	err := s.db.WithContext(ctx).First(&n, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// ListNotificationsFilter filters ListNotifications.
type ListNotificationsFilter struct {
	Channel string
	Status  string
}

// ListNotifications lists notifications, newest first.
func (s *NotificationService) ListNotifications(ctx context.Context, filter ListNotificationsFilter) ([]Notification, error) {
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if filter.Channel != "" {
		query = query.Where("channel = ?", filter.Channel)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var notifications []Notification
	if err := query.Find(&notifications).Error; err != nil {
		return nil, err
	}
	return notifications, nil
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
	var notifications []Notification
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("channel = ?", ChannelInApp).
			Where("recipient ->> 'user_id' = ?", userID).
			Where("read_at IS NULL").
			Order("created_at DESC").
			Find(&notifications).Error; err != nil {
			return err
		}
		if len(notifications) == 0 {
			return nil
		}

		ids := make([]uuid.UUID, len(notifications))
		for i := range notifications {
			ids[i] = notifications[i].ID
		}
		return tx.Model(&Notification{}).Where("id IN ?", ids).Update("read_at", time.Now()).Error
	})
	if err != nil {
		return nil, err
	}
	return notifications, nil
}

// markProcessing/markSent/markRetrying/markFailed are worker-facing status
// transitions, unexported since only the worker's send path drives them.

func (s *NotificationService) markProcessing(ctx context.Context, id uuid.UUID) error {
	return s.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{"status": StatusProcessing}).Error
}

func (s *NotificationService) markSent(ctx context.Context, id uuid.UUID, provider, providerMessageID string) error {
	return s.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":              StatusSent,
			"provider":            provider,
			"provider_message_id": providerMessageID,
			"error":               nil,
		}).Error
}

func (s *NotificationService) markRetrying(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return s.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  StatusRetrying,
			"attempt": attempt,
			"error":   sendErr.Error(),
		}).Error
}

func (s *NotificationService) markFailed(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return s.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  StatusFailed,
			"attempt": attempt,
			"error":   sendErr.Error(),
		}).Error
}
