package notification

import (
	"context"
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/google/uuid"
)

// SendQueueName is the DBOS queue notification sends run on.
const SendQueueName = "notification-send"

// SendPayload is SendWorkflow's input. It only carries the notification
// ID - the workflow always re-reads current state from Postgres (source of
// truth) rather than trusting a possibly-stale payload.
type SendPayload struct {
	NotificationID uuid.UUID
}

// maxRetry bounds how many times a send step will be retried before the
// notification is marked failed.
const maxRetry = 5

// TaskEnqueuer implements Enqueuer by starting (or, for an already-completed
// notification ID, safely no-op'ing against) a DBOS workflow run per
// notification. NewTaskEnqueuer registers the workflow/queue itself, so it
// must be constructed before dbosCtx is launched.
type TaskEnqueuer struct {
	ctx          dbos.Context
	queue        dbos.Queue
	sendWorkflow dbos.Workflow[SendPayload, sendWorkflowResult]
}

// NewTaskEnqueuer registers worker.SendWorkflow and the queue it runs on
// against ctx.
func NewTaskEnqueuer(ctx dbos.Context, worker *Worker) (*TaskEnqueuer, error) {
	dbos.RegisterWorkflow(ctx, worker.SendWorkflow)
	queue, err := dbos.RegisterQueue(ctx, SendQueueName)
	if err != nil {
		return nil, fmt.Errorf("register %s queue: %w", SendQueueName, err)
	}
	return &TaskEnqueuer{ctx: ctx, queue: queue, sendWorkflow: worker.SendWorkflow}, nil
}

// EnqueueSend starts SendWorkflow for notificationID, using the notification
// ID itself as the DBOS workflow ID so re-enqueuing an in-flight or
// already-completed notification is idempotent.
func (e *TaskEnqueuer) EnqueueSend(_ context.Context, notificationID uuid.UUID) error {
	_, err := dbos.RunWorkflow(e.ctx, e.sendWorkflow, SendPayload{NotificationID: notificationID},
		dbos.WithQueue(e.queue),
		dbos.WithWorkflowID(notificationID.String()),
	)
	if err != nil {
		return fmt.Errorf("enqueue send workflow: %w", err)
	}
	return nil
}
