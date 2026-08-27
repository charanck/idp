package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/notification"
)

type gormNotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *gormNotificationRepository {
	return &gormNotificationRepository{db: db}
}

func (r *gormNotificationRepository) FindByIdempotencyKey(ctx context.Context, key string) (*model.Notification, error) {
	var n model.Notification
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *gormNotificationRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Notification, error) {
	var n model.Notification
	err := r.db.WithContext(ctx).First(&n, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *gormNotificationRepository) List(ctx context.Context, filter model.ListNotificationsFilter) ([]model.Notification, error) {
	query := r.db.WithContext(ctx).Order("created_at DESC")
	if filter.Channel != "" {
		query = query.Where("channel = ?", filter.Channel)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var notifications []model.Notification
	if err := query.Find(&notifications).Error; err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *gormNotificationRepository) Create(ctx context.Context, n *model.Notification) error {
	return r.db.WithContext(ctx).Create(n).Error
}

func (r *gormNotificationRepository) ConsumeUnreadInApp(ctx context.Context, userID string) ([]model.Notification, error) {
	var notifications []model.Notification
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("channel = ?", model.ChannelInApp).
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
		return tx.Model(&model.Notification{}).Where("id IN ?", ids).Update("read_at", time.Now()).Error
	})
	if err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *gormNotificationRepository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&model.Notification{}).Where("id = ?", id).
		Updates(map[string]any{"status": model.StatusProcessing}).Error
}

func (r *gormNotificationRepository) MarkSent(ctx context.Context, id uuid.UUID, provider, providerMessageID string) error {
	return r.db.WithContext(ctx).Model(&model.Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":              model.StatusSent,
			"provider":            provider,
			"provider_message_id": providerMessageID,
			"error":               nil,
		}).Error
}

func (r *gormNotificationRepository) MarkRetrying(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return r.db.WithContext(ctx).Model(&model.Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  model.StatusRetrying,
			"attempt": attempt,
			"error":   sendErr.Error(),
		}).Error
}

func (r *gormNotificationRepository) MarkFailed(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	return r.db.WithContext(ctx).Model(&model.Notification{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  model.StatusFailed,
			"attempt": attempt,
			"error":   sendErr.Error(),
		}).Error
}

var _ model.NotificationRepository = (*gormNotificationRepository)(nil)
