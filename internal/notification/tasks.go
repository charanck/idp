package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// TaskTypeSend is the single asynq task type this module uses. The payload
// only carries the notification ID - the worker always re-reads current
// state from Postgres (source of truth) rather than trusting a possibly-
// stale payload.
const TaskTypeSend = "notification:send"

// sendTaskPayload is the JSON payload for TaskTypeSend.
type sendTaskPayload struct {
	NotificationID uuid.UUID `json:"notification_id"`
}

// maxRetry bounds how many times asynq will retry a failed send task.
const maxRetry = 5

// TaskEnqueuer implements Enqueuer using an asynq.Client.
type TaskEnqueuer struct {
	client *asynq.Client
}

func NewTaskEnqueuer(client *asynq.Client) *TaskEnqueuer {
	return &TaskEnqueuer{client: client}
}

func (e *TaskEnqueuer) EnqueueSend(ctx context.Context, notificationID uuid.UUID) error {
	payload, err := json.Marshal(sendTaskPayload{NotificationID: notificationID})
	if err != nil {
		return fmt.Errorf("marshal send task payload: %w", err)
	}
	task := asynq.NewTask(TaskTypeSend, payload)
	if _, err := e.client.EnqueueContext(ctx, task, asynq.MaxRetry(maxRetry)); err != nil {
		return fmt.Errorf("enqueue send task: %w", err)
	}
	return nil
}
