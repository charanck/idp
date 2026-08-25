package notification

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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

type gormNotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *gormNotificationRepository {
	return &gormNotificationRepository{db: db}
}

func (r *gormNotificationRepository) FindByIdempotencyKey(ctx context.Context, key string) (*Notification, error) {
	var n Notification
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *gormNotificationRepository) FindByID(ctx context.Context, id uuid.UUID) (*Notification, error) {
	var n Notification
	err := r.db.WithContext(ctx).First(&n, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *gormNotificationRepository) List(ctx context.Context, filter ListNotificationsFilter) ([]Notification, error) {
	query := r.db.WithContext(ctx).Order("created_at DESC")
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

func (r *gormNotificationRepository) Create(ctx context.Context, n *Notification) error {
	return r.db.WithContext(ctx).Create(n).Error
}

func (r *gormNotificationRepository) ConsumeUnreadInApp(ctx context.Context, userID string) ([]Notification, error) {
	var notifications []Notification
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

func (r *gormNotificationRepository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{"status": StatusProcessing}).Error
}

func (r *gormNotificationRepository) MarkSent(ctx context.Context, id uuid.UUID, provider, providerMessageID string) error {
	return r.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":              StatusSent,
			"provider":            provider,
			"provider_message_id": providerMessageID,
			"error":               nil,
		}).Error
}

func (r *gormNotificationRepository) MarkRetrying(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return r.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  StatusRetrying,
			"attempt": attempt,
			"error":   sendErr.Error(),
		}).Error
}

func (r *gormNotificationRepository) MarkFailed(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return r.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  StatusFailed,
			"attempt": attempt,
			"error":   sendErr.Error(),
		}).Error
}

var _ NotificationRepository = (*gormNotificationRepository)(nil)
