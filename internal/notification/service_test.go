package notification_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	model "controlplane/internal/model/notification"
	"controlplane/internal/notification"
)

// fakeEnqueuer records EnqueueSend calls instead of running a real DBOS
// workflow, so NotificationService's logic can be tested independently of
// DBOS/Postgres.
type fakeEnqueuer struct {
	mu       sync.Mutex
	enqueued []uuid.UUID
}

func (f *fakeEnqueuer) EnqueueSend(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueued = append(f.enqueued, id)
	return nil
}

func (f *fakeEnqueuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.enqueued)
}

func TestCreateNotification_InsertsQueuedAndEnqueues(t *testing.T) {
	repo := newFakeNotificationRepository()
	enqueuer := &fakeEnqueuer{}
	svc := notification.NewNotificationService(repo, newFakeApplicationRepository(), enqueuer)

	n, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel:   model.ChannelEmail,
		Recipient: datatypes.JSON(`{"email":"a@example.com"}`),
		Content:   datatypes.JSON(`{"subject":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if n.Status != model.StatusQueued {
		t.Fatalf("status = %q, want queued", n.Status)
	}
	if enqueuer.count() != 1 {
		t.Fatalf("enqueued %d times, want 1", enqueuer.count())
	}

	got, err := svc.GetNotification(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got == nil || got.ID != n.ID {
		t.Fatalf("GetNotification returned %+v", got)
	}
}

func TestCreateNotification_IdempotentQueuedReEnqueues(t *testing.T) {
	repo := newFakeNotificationRepository()
	enqueuer := &fakeEnqueuer{}
	svc := notification.NewNotificationService(repo, newFakeApplicationRepository(), enqueuer)
	ctx := context.Background()

	in := notification.CreateNotificationInput{
		Channel:        model.ChannelInApp,
		Recipient:      datatypes.JSON(`{"user_id":"u1"}`),
		Content:        datatypes.JSON(`{"message":"hi"}`),
		IdempotencyKey: "key-1",
	}

	first, err := svc.CreateNotification(ctx, in)
	if err != nil {
		t.Fatalf("CreateNotification (1st): %v", err)
	}
	if enqueuer.count() != 1 {
		t.Fatalf("expected 1 enqueue after first create, got %d", enqueuer.count())
	}

	second, err := svc.CreateNotification(ctx, in)
	if err != nil {
		t.Fatalf("CreateNotification (2nd): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same notification returned for repeated idempotency key")
	}
	// Both the initial create and the idempotent re-enqueue should have
	// enqueued (still-queued notifications are best-effort re-enqueued).
	if enqueuer.count() != 2 {
		t.Fatalf("expected re-enqueue on 2nd create of still-queued notification, got %d enqueue calls", enqueuer.count())
	}
}

func TestGetNotification_ReturnsNilForUnknownID(t *testing.T) {
	svc := notification.NewNotificationService(newFakeNotificationRepository(), newFakeApplicationRepository(), &fakeEnqueuer{})

	got, err := svc.GetNotification(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestListNotifications_FiltersByChannelAndStatus(t *testing.T) {
	svc := notification.NewNotificationService(newFakeNotificationRepository(), newFakeApplicationRepository(), &fakeEnqueuer{})

	if _, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: model.ChannelEmail, Recipient: datatypes.JSON(`{}`), Content: datatypes.JSON(`{}`),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if _, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: model.ChannelSMS, Recipient: datatypes.JSON(`{}`), Content: datatypes.JSON(`{}`),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	emails, err := svc.ListNotifications(context.Background(), model.ListNotificationsFilter{Channel: model.ChannelEmail})
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(emails) != 1 || emails[0].Channel != model.ChannelEmail {
		t.Fatalf("emails = %+v", emails)
	}

	queued, err := svc.ListNotifications(context.Background(), model.ListNotificationsFilter{Status: model.StatusQueued})
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("queued = %d, want 2", len(queued))
	}
}

func TestConsumeUnreadInAppForUser_OnlyInAppChannelAndMarksRead(t *testing.T) {
	repo := newFakeNotificationRepository()
	svc := notification.NewNotificationService(repo, newFakeApplicationRepository(), &fakeEnqueuer{})
	ctx := context.Background()

	mustCreate := func(channel, userID string) *model.Notification {
		n, err := svc.CreateNotification(ctx, notification.CreateNotificationInput{
			Channel:   channel,
			Recipient: datatypes.JSON(`{"user_id":"` + userID + `"}`),
			Content:   datatypes.JSON(`{"message":"hi"}`),
		})
		if err != nil {
			t.Fatalf("CreateNotification: %v", err)
		}
		return n
	}

	inApp := mustCreate(model.ChannelInApp, "u1")
	mustCreate(model.ChannelEmail, "u1")
	mustCreate(model.ChannelInApp, "u2")

	unread, err := svc.ConsumeUnreadInAppForUser(ctx, "u1", inApp.ApplicationID)
	if err != nil {
		t.Fatalf("ConsumeUnreadInAppForUser: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread notification for u1, got %d", len(unread))
	}
	if unread[0].ID != inApp.ID {
		t.Fatalf("expected the inapp notification, got %+v", unread[0])
	}
	if unread[0].ReadAt == nil {
		t.Fatal("expected ReadAt to be set")
	}

	// A second call should see nothing left unread - the first call marked
	// what it returned as read.
	againUnread, err := svc.ConsumeUnreadInAppForUser(ctx, "u1", inApp.ApplicationID)
	if err != nil {
		t.Fatalf("ConsumeUnreadInAppForUser (2nd): %v", err)
	}
	if len(againUnread) != 0 {
		t.Fatalf("expected 0 unread on 2nd call, got %d", len(againUnread))
	}
}
