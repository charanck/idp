package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	model "controlplane/internal/model/notification"
	"controlplane/internal/notification/provider"
)

// Worker processes TaskTypeSend tasks: send via the channel's provider, then
// mirror the outcome into the notifications table and (if hub is non-nil)
// publish the final status over SSE.
type Worker struct {
	notifications *NotificationService
	settings      *ProviderSettingService
	channels      ChannelRegistry
	hub           *Hub
}

func NewWorker(notifications *NotificationService, settings *ProviderSettingService, channels ChannelRegistry, hub *Hub) *Worker {
	return &Worker{notifications: notifications, settings: settings, channels: channels, hub: hub}
}

// publish publishes n's current in-memory Status/Provider over SSE, if a hub
// is configured. Errors are logged, not returned - a failed SSE publish
// should never fail the send task itself (the row in Postgres is already
// the source of truth, and SSE is fire-and-forget by design).
func (w *Worker) publish(ctx context.Context, n *model.Notification) {
	if w.hub == nil {
		return
	}
	if err := w.hub.PublishSent(ctx, n); err != nil {
		slog.Error("publish sse event failed", "id", n.ID, "err", err)
	}
}

// HandleSend implements asynq.HandlerFunc for TaskTypeSend.
func (w *Worker) HandleSend(ctx context.Context, task *asynq.Task) error {
	var payload sendTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal send task payload: %w", err)
	}

	n, err := w.notifications.GetNotification(ctx, payload.NotificationID)
	if err != nil {
		return fmt.Errorf("load notification %s: %w", payload.NotificationID, err)
	}
	if n == nil {
		slog.Warn("notification send task: notification no longer exists", "id", payload.NotificationID)
		return nil
	}
	// A notification already past "queued"/"retrying" was handled by a
	// previous delivery of this task (or a re-enqueue race) - skip.
	if n.Status != model.StatusQueued && n.Status != model.StatusRetrying {
		return nil
	}

	if err := w.notifications.markProcessing(ctx, n.ID); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	channel, ok := w.channels[n.Channel]
	if !ok {
		permErr := fmt.Errorf("no channel registered for %q", n.Channel)
		if markErr := w.notifications.markFailed(ctx, n.ID, n.Attempt, permErr); markErr != nil {
			slog.Error("mark failed after unknown channel", "id", n.ID, "err", markErr)
		}
		n.Status = model.StatusFailed
		w.publish(ctx, n)
		return fmt.Errorf("%w: %v", asynq.SkipRetry, permErr)
	}

	if w.settings != nil {
		setting, err := w.settings.Get(ctx, n.Channel)
		if err != nil {
			return fmt.Errorf("load provider setting for %q: %w", n.Channel, err)
		}
		if setting != nil && !setting.IsActive {
			permErr := fmt.Errorf("channel %q is disabled", n.Channel)
			if markErr := w.notifications.markFailed(ctx, n.ID, n.Attempt, permErr); markErr != nil {
				slog.Error("mark failed after disabled channel", "id", n.ID, "err", markErr)
			}
			n.Status = model.StatusFailed
			w.publish(ctx, n)
			return fmt.Errorf("%w: %v", asynq.SkipRetry, permErr)
		}
	}

	result, sendErr := channel.Send(ctx, provider.Notification{Recipient: n.Recipient, Content: n.Content})
	attempt := n.Attempt + 1
	if sendErr != nil {
		return w.handleSendError(ctx, n, attempt, sendErr)
	}

	if err := w.notifications.markSent(ctx, n.ID, result.Provider, result.ProviderMessageID); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}
	n.Status = model.StatusSent
	n.Provider = &result.Provider
	w.publish(ctx, n)

	return nil
}

// handleSendError mirrors asynq's own retry decision into the notifications
// table: retrying while asynq still has attempts left, failed once asynq's
// retry counter (or a permanent SendError) is exhausted. Permanent failures
// wrap the error with asynq.SkipRetry so asynq doesn't requeue them, and
// (since they're the final status) get published over SSE.
func (w *Worker) handleSendError(ctx context.Context, n *model.Notification, attempt int, sendErr error) error {
	var permanent bool
	var sendErrTyped *provider.SendError
	if errors.As(sendErr, &sendErrTyped) {
		permanent = !sendErrTyped.Transient
	}

	retryCount, hasRetryCount := asynq.GetRetryCount(ctx)
	maxRetryCount, hasMaxRetry := asynq.GetMaxRetry(ctx)
	exhausted := hasRetryCount && hasMaxRetry && retryCount >= maxRetryCount

	if permanent || exhausted {
		if err := w.notifications.markFailed(ctx, n.ID, attempt, sendErr); err != nil {
			slog.Error("mark failed", "id", n.ID, "err", err)
		}
		n.Status = model.StatusFailed
		w.publish(ctx, n)
		if permanent {
			return fmt.Errorf("%w: %v", asynq.SkipRetry, sendErr)
		}
		return sendErr
	}

	if err := w.notifications.markRetrying(ctx, n.ID, attempt, sendErr); err != nil {
		slog.Error("mark retrying", "id", n.ID, "err", err)
	}
	return sendErr
}
