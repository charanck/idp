package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	err := r.db.WithContext(ctx).Preload("Application").Where("idempotency_key = ?", key).First(&n).Error
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
	err := r.db.WithContext(ctx).Preload("Application").First(&n, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *gormNotificationRepository) List(ctx context.Context, filter model.ListNotificationsFilter) ([]model.Notification, error) {
	query := r.db.WithContext(ctx).Preload("Application").Order("notifications.created_at DESC")
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.ApplicationID != nil {
		query = query.Where("notifications.application_id = ?", *filter.ApplicationID)
	}
	if filter.Service != "" {
		query = query.Joins("JOIN applications ON applications.id = notifications.application_id").
			Where("applications.name = ?", filter.Service)
	}
	if filter.Channel != "" {
		query = query.Where("notifications.channel = ?", filter.Channel)
	}
	if filter.Status != "" {
		query = query.Where("notifications.status = ?", filter.Status)
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

func (r *gormNotificationRepository) ListInApp(ctx context.Context, userID string, applicationID uuid.UUID) ([]model.Notification, error) {
	var notifications []model.Notification
	if err := r.db.WithContext(ctx).
		Preload("Application").
		Where("application_id = ?", applicationID).
		Where("channel = ?", model.ChannelInApp).
		Where("recipient ->> 'user_id' = ?", userID).
		Order("created_at DESC").
		Find(&notifications).Error; err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *gormNotificationRepository) GetUnreadInApp(ctx context.Context, userID string, applicationID uuid.UUID) ([]model.Notification, error) {
	var notifications []model.Notification
	if err := r.db.WithContext(ctx).
		Preload("Application").
		Where("application_id = ?", applicationID).
		Where("channel = ?", model.ChannelInApp).
		Where("recipient ->> 'user_id' = ?", userID).
		Where("read_at IS NULL").
		Order("created_at DESC").
		Find(&notifications).Error; err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *gormNotificationRepository) ConsumeUnreadInApp(ctx context.Context, userID string, applicationID uuid.UUID) ([]model.Notification, error) {
	var notifications []model.Notification
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE: two concurrent consumers must not both read (and both
		// return) the same unread rows before either marks them read.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Preload("Application").
			Where("application_id = ?", applicationID).
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

func (r *gormNotificationRepository) MarkInAppAsRead(ctx context.Context, userID string, applicationID uuid.UUID, notificationIDs []uuid.UUID) error {
	if len(notificationIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id IN ?", notificationIDs).
		Where("application_id = ?", applicationID).
		Where("channel = ?", model.ChannelInApp).
		Where("recipient ->> 'user_id' = ?", userID).
		Where("read_at IS NULL").
		Update("read_at", time.Now()).Error
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

func (r *gormNotificationRepository) DeleteOlderThan(ctx context.Context, before time.Time) error {
	// Bounded batches; see activity repository's DeleteOlderThan.
	for {
		res := r.db.WithContext(ctx).Exec(
			`DELETE FROM notifications WHERE ctid IN (SELECT ctid FROM notifications WHERE created_at < ? LIMIT ?)`,
			before, deleteBatchSize)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected < deleteBatchSize {
			return nil
		}
	}
}

// deleteBatchSize bounds how many rows a single retention DELETE removes.
const deleteBatchSize = 5000

var _ model.NotificationRepository = (*gormNotificationRepository)(nil)
