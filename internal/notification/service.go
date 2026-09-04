package notification

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/datatypes"

	configmodel "controlplane/internal/model/config"
	model "controlplane/internal/model/notification"
)

// NotificationService creates and reads notifications, and enqueues them for
// asynchronous delivery.
type NotificationService struct {
	repo     model.NotificationRepository
	apps     configmodel.ApplicationRepository
	enqueuer Enqueuer
}

// Enqueuer schedules a notification for delivery. Satisfied by *TaskEnqueuer.
type Enqueuer interface {
	EnqueueSend(ctx context.Context, notificationID uuid.UUID) error
}

func NewNotificationService(repo model.NotificationRepository, apps configmodel.ApplicationRepository, enqueuer Enqueuer) *NotificationService {
	return &NotificationService{repo: repo, apps: apps, enqueuer: enqueuer}
}

// SetEnqueuer replaces the service's enqueuer after construction. Exists
// only for cmd/server's wiring: a DBOS-backed Enqueuer needs a *Worker,
// which itself needs to be constructed from this NotificationService,
// forming a cycle that NewNotificationService's constructor can't resolve.
// Callers everywhere else should pass the enqueuer to NewNotificationService
// directly.
func (s *NotificationService) SetEnqueuer(e Enqueuer) {
	s.enqueuer = e
}

// CreateNotificationInput bundles CreateNotification's parameters.
type CreateNotificationInput struct {
	Service        string
	Channel        string
	Recipient      datatypes.JSON
	Content        datatypes.JSON
	IdempotencyKey string
}

// CreateNotification resolves/creates the Service's Application scope,
// inserts a queued notification under it, and enqueues it for delivery. If
// IdempotencyKey is set and already exists: a still-queued match is
// best-effort re-enqueued and returned unchanged; a match past "queued" is
// returned as-is (no-op). A unique-violation race on insert (two concurrent
// creates racing on the same IdempotencyKey) is treated the same as "found"
// by re-reading the existing row.
func (s *NotificationService) CreateNotification(ctx context.Context, in CreateNotificationInput) (*model.Notification, error) {
	if in.IdempotencyKey != "" {
		existing, err := s.repo.FindByIdempotencyKey(ctx, in.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.Status == model.StatusQueued {
				if err := s.enqueuer.EnqueueSend(ctx, existing.ID); err != nil {
					slog.Error("re-enqueue idempotent notification failed", "id", existing.ID, "err", err)
				}
			}
			return existing, nil
		}
	}

	app, err := s.apps.GetOrCreate(ctx, in.Service)
	if err != nil {
		return nil, err
	}

	n := &model.Notification{
		ApplicationID: app.ID,
		Channel:       in.Channel,
		Recipient:     in.Recipient,
		Content:       in.Content,
		Status:        model.StatusQueued,
	}
	if in.IdempotencyKey != "" {
		key := in.IdempotencyKey
		n.IdempotencyKey = &key
	}

	if err := s.repo.Create(ctx, n); err != nil {
		if in.IdempotencyKey != "" && isUniqueViolation(err) {
			existing, findErr := s.repo.FindByIdempotencyKey(ctx, in.IdempotencyKey)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				if existing.Status == model.StatusQueued {
					if err := s.enqueuer.EnqueueSend(ctx, existing.ID); err != nil {
						slog.Error("re-enqueue idempotent notification failed", "id", existing.ID, "err", err)
					}
				}
				return existing, nil
			}
		}
		return nil, err
	}
	n.Application = *app

	if err := s.enqueuer.EnqueueSend(ctx, n.ID); err != nil {
		return nil, err
	}

	slog.Info("created notification", "id", n.ID, "channel", n.Channel)
	return n, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) - the shape of the race CreateNotification
// re-reads around when two concurrent requests share an IdempotencyKey.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// GetNotification returns a notification by ID, or nil if not found.
func (s *NotificationService) GetNotification(ctx context.Context, id uuid.UUID) (*model.Notification, error) {
	return s.repo.FindByID(ctx, id)
}

// ListNotifications lists notifications, newest first.
func (s *NotificationService) ListNotifications(ctx context.Context, filter model.ListNotificationsFilter) ([]model.Notification, error) {
	return s.repo.List(ctx, filter)
}

// ConsumeUnreadInAppForUser lists a user's unread InApp notifications under
// applicationID, newest first, and marks them read in the same transaction -
// e.g. so a client catching up on missed notifications only ever sees each
// one once. Scoped to ChannelInApp only: InApp is the sole channel with a
// pull-based inbox, unlike email/sms (fire-and-forget delivery) or
// SSE (never persisted at all). The user is matched by the "user_id" field
// in the jsonb Recipient blob, the same external identifier the
// notification session token uses; applicationID is the application that
// token was minted for, so a token can't read another application's
// notifications for the same user_id.
func (s *NotificationService) ConsumeUnreadInAppForUser(ctx context.Context, userID string, applicationID uuid.UUID) ([]model.Notification, error) {
	return s.repo.ConsumeUnreadInApp(ctx, userID, applicationID)
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
