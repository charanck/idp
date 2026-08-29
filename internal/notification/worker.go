package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/google/uuid"

	model "controlplane/internal/model/notification"
	"controlplane/internal/notification/provider"
)

// Publisher is the narrow slice of *Hub that Worker needs, satisfied by
// *Hub. Narrowed to an interface (rather than a concrete *Hub field) so
// worker tests can fake it without a real Redis.
type Publisher interface {
	PublishSent(ctx context.Context, n *model.Notification) error
}

// Worker processes notification sends: send via the channel's provider, then
// mirror the outcome into the notifications table and (if hub is non-nil)
// publish the final status over SSE.
type Worker struct {
	notifications *NotificationService
	settings      *ProviderSettingService
	channels      ChannelRegistry
	hub           Publisher
}

func NewWorker(notifications *NotificationService, settings *ProviderSettingService, channels ChannelRegistry, hub Publisher) *Worker {
	return &Worker{notifications: notifications, settings: settings, channels: channels, hub: hub}
}

// publish publishes n's current in-memory Status/Provider over SSE, if a hub
// is configured. Restricted to ChannelInApp: SSE is a live nudge for the
// pull-based in-app inbox, not a generic delivery-status feed for
// email/sms/whatsapp. Errors are logged, not returned - a failed SSE publish
// should never fail the send workflow itself (the row in Postgres is already
// the source of truth, and SSE is fire-and-forget by design).
func (w *Worker) publish(ctx context.Context, n *model.Notification) {
	if w.hub == nil || n.Channel != model.ChannelInApp {
		return
	}
	if err := w.hub.PublishSent(ctx, n); err != nil {
		slog.Error("publish sse event failed", "id", n.ID, "err", err)
	}
}

// sendWorkflowResult is SendWorkflow's DBOS return value. All outcome state
// lives in the notifications table (the source of truth), not here - DBOS
// workflows just require a result type.
type sendWorkflowResult struct{}

// The stepX methods below are SendWorkflow's non-deterministic operations
// (DB reads/writes), each wrapped in its own dbos.RunAsStep so DBOS
// checkpoints and can safely replay them - SendWorkflow itself is left as
// deterministic orchestration over these steps plus in-memory logic
// (status checks, the channel registry lookup).

func (w *Worker) stepLoadNotification(ctx dbos.Context, id uuid.UUID) (*model.Notification, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (*model.Notification, error) {
		return w.notifications.GetNotification(stepCtx, id)
	})
}

func (w *Worker) stepMarkProcessing(ctx dbos.Context, id uuid.UUID) (struct{}, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (struct{}, error) {
		return struct{}{}, w.notifications.markProcessing(stepCtx, id)
	})
}

// stepLoadProviderSetting wraps only the setting lookup. The subsequent
// DecryptCredentials call is deliberately left as plain (non-step) code at
// its call site: it's pure decryption of already-loaded ciphertext, with no
// I/O and no ctx param, so it's deterministic given the same setting and
// doesn't need step-checkpointing.
func (w *Worker) stepLoadProviderSetting(ctx dbos.Context, channel string) (*model.ProviderSetting, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (*model.ProviderSetting, error) {
		return w.settings.Get(stepCtx, channel)
	})
}

func (w *Worker) stepMarkFailed(ctx dbos.Context, id uuid.UUID, attempt int, sendErr error) (struct{}, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (struct{}, error) {
		return struct{}{}, w.notifications.markFailed(stepCtx, id, attempt, sendErr)
	})
}

func (w *Worker) stepMarkSent(ctx dbos.Context, id uuid.UUID, providerName, providerMessageID string) (struct{}, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (struct{}, error) {
		return struct{}{}, w.notifications.markSent(stepCtx, id, providerName, providerMessageID)
	})
}

// stepPublish wraps w.publish, which already swallows/logs its own errors
// (a failed SSE publish must never fail the send workflow) - so this step
// always returns nil, existing purely to checkpoint the publish attempt.
func (w *Worker) stepPublish(ctx dbos.Context, n *model.Notification) (struct{}, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (struct{}, error) {
		w.publish(stepCtx, n)
		return struct{}{}, nil
	})
}

// SendWorkflow is the DBOS workflow TaskEnqueuer runs per notification, keyed
// by the notification's own UUID as the DBOS workflow ID - re-running it for
// an already-completed notification is a safe no-op (DBOS returns the
// recorded result instead of re-executing), which is what gives sends
// exactly-once processing without a separate dedupe check.
func (w *Worker) SendWorkflow(ctx dbos.Context, payload SendPayload) (sendWorkflowResult, error) {
	n, err := w.stepLoadNotification(ctx, payload.NotificationID)
	if err != nil {
		return sendWorkflowResult{}, fmt.Errorf("load notification %s: %w", payload.NotificationID, err)
	}
	if n == nil {
		slog.Warn("notification send workflow: notification no longer exists", "id", payload.NotificationID)
		return sendWorkflowResult{}, nil
	}
	// A notification already past "queued"/"retrying" was handled by a
	// previous run of this workflow ID - skip.
	if n.Status != model.StatusQueued && n.Status != model.StatusRetrying {
		return sendWorkflowResult{}, nil
	}

	if _, err := w.stepMarkProcessing(ctx, n.ID); err != nil {
		return sendWorkflowResult{}, fmt.Errorf("mark processing: %w", err)
	}

	channel, ok := w.channels[n.Channel]
	if !ok {
		permErr := fmt.Errorf("no channel registered for %q", n.Channel)
		if _, markErr := w.stepMarkFailed(ctx, n.ID, n.Attempt, permErr); markErr != nil {
			slog.Error("mark failed after unknown channel", "id", n.ID, "err", markErr)
		}
		n.Status = model.StatusFailed
		w.stepPublish(ctx, n) //nolint:errcheck // stepPublish never returns a non-nil error, see its doc comment.
		return sendWorkflowResult{}, permErr
	}

	var settings provider.Settings
	if w.settings != nil {
		setting, err := w.stepLoadProviderSetting(ctx, n.Channel)
		if err != nil {
			return sendWorkflowResult{}, fmt.Errorf("load provider setting for %q: %w", n.Channel, err)
		}
		if setting != nil {
			if !setting.IsActive {
				permErr := fmt.Errorf("channel %q is disabled", n.Channel)
				if _, markErr := w.stepMarkFailed(ctx, n.ID, n.Attempt, permErr); markErr != nil {
					slog.Error("mark failed after disabled channel", "id", n.ID, "err", markErr)
				}
				n.Status = model.StatusFailed
				w.stepPublish(ctx, n) //nolint:errcheck // stepPublish never returns a non-nil error, see its doc comment.
				return sendWorkflowResult{}, permErr
			}
			settings.Config = setting.Config
			credentials, err := w.settings.DecryptCredentials(setting)
			if err != nil {
				return sendWorkflowResult{}, fmt.Errorf("decrypt provider credentials for %q: %w", n.Channel, err)
			}
			settings.Credentials = credentials
		}
	}

	// attempt tracks the notification's attempt count across the step's
	// internal retries - the retry predicate bumps it and records a
	// "retrying" status on every transient failure, mirroring what used to
	// happen once per asynq task redelivery. markRetrying is called directly
	// (not via a stepX wrapper) because it runs inside channel.Send's own
	// step's retry predicate - DBOS steps can't nest - and the write is
	// naturally idempotent (it only updates attempt/error columns).
	attempt := n.Attempt
	result, sendErr := dbos.RunAsStep(ctx, func(stepCtx context.Context) (*provider.Result, error) {
		return channel.Send(stepCtx, provider.Notification{Recipient: n.Recipient, Content: n.Content}, settings)
	},
		dbos.WithStepMaxRetries(maxRetry),
		dbos.WithStepRetryPredicate(func(sendErr error) bool {
			var sendErrTyped *provider.SendError
			if errors.As(sendErr, &sendErrTyped) && !sendErrTyped.Transient {
				return false
			}
			attempt++
			if markErr := w.notifications.markRetrying(ctx, n.ID, attempt, sendErr); markErr != nil {
				slog.Error("mark retrying", "id", n.ID, "err", markErr)
			}
			return true
		}),
	)
	if sendErr != nil {
		// RunAsStep only returns once its retries are exhausted (or the
		// predicate rejected a permanent error), so this is always terminal.
		if _, err := w.stepMarkFailed(ctx, n.ID, attempt+1, sendErr); err != nil {
			slog.Error("mark failed", "id", n.ID, "err", err)
		}
		n.Status = model.StatusFailed
		w.stepPublish(ctx, n) //nolint:errcheck // stepPublish never returns a non-nil error, see its doc comment.
		return sendWorkflowResult{}, sendErr
	}

	if _, err := w.stepMarkSent(ctx, n.ID, result.Provider, result.ProviderMessageID); err != nil {
		return sendWorkflowResult{}, fmt.Errorf("mark sent: %w", err)
	}
	n.Status = model.StatusSent
	n.Provider = &result.Provider
	w.stepPublish(ctx, n) //nolint:errcheck // stepPublish never returns a non-nil error, see its doc comment.

	return sendWorkflowResult{}, nil
}
