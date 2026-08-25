package notification_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"controlplane/internal/notification"
	"controlplane/internal/testutil"
)

// fakeEnqueuer records EnqueueSend calls instead of talking to Redis, so
// NotificationService's logic can be tested independently of asynq.
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
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	enqueuer := &fakeEnqueuer{}
	svc := notification.NewNotificationService(notification.NewNotificationRepository(gdb), enqueuer)

	n, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel:   notification.ChannelEmail,
		Recipient: datatypes.JSON(`{"email":"a@example.com"}`),
		Content:   datatypes.JSON(`{"subject":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if n.Status != notification.StatusQueued {
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

func TestCreateNotification_IdempotencyKeyReturnsExistingQueued(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	enqueuer := &fakeEnqueuer{}
	svc := notification.NewNotificationService(notification.NewNotificationRepository(gdb), enqueuer)

	in := notification.CreateNotificationInput{
		Channel:        notification.ChannelSMS,
		Recipient:      datatypes.JSON(`{"phone":"+10000000000"}`),
		Content:        datatypes.JSON(`{"body":"hi"}`),
		IdempotencyKey: "order-123",
	}
	first, err := svc.CreateNotification(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateNotification (first): %v", err)
	}

	second, err := svc.CreateNotification(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateNotification (second): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second call created a new row: %s != %s", second.ID, first.ID)
	}
	// Both the initial create and the idempotent re-enqueue should have
	// enqueued (still-queued notifications are best-effort re-enqueued).
	if enqueuer.count() != 2 {
		t.Fatalf("enqueued %d times, want 2", enqueuer.count())
	}
}

func TestGetNotification_ReturnsNilForUnknownID(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	svc := notification.NewNotificationService(notification.NewNotificationRepository(gdb), &fakeEnqueuer{})

	got, err := svc.GetNotification(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestListNotifications_FiltersByChannelAndStatus(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	svc := notification.NewNotificationService(notification.NewNotificationRepository(gdb), &fakeEnqueuer{})

	if _, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: notification.ChannelEmail, Recipient: datatypes.JSON(`{}`), Content: datatypes.JSON(`{}`),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if _, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: notification.ChannelSMS, Recipient: datatypes.JSON(`{}`), Content: datatypes.JSON(`{}`),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	emails, err := svc.ListNotifications(context.Background(), notification.ListNotificationsFilter{Channel: notification.ChannelEmail})
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(emails) != 1 || emails[0].Channel != notification.ChannelEmail {
		t.Fatalf("emails = %+v", emails)
	}

	queued, err := svc.ListNotifications(context.Background(), notification.ListNotificationsFilter{Status: notification.StatusQueued})
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("queued = %d, want 2", len(queued))
	}
}

func TestConsumeUnreadInAppForUser_FiltersByChannelAndRecipientUserIDAndMarksReturnedAsRead(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	svc := notification.NewNotificationService(notification.NewNotificationRepository(gdb), &fakeEnqueuer{})

	unreadForUser1, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: notification.ChannelInApp, Recipient: datatypes.JSON(`{"user_id":"user-1"}`), Content: datatypes.JSON(`{"title":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	alreadyReadForUser1, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: notification.ChannelInApp, Recipient: datatypes.JSON(`{"user_id":"user-1"}`), Content: datatypes.JSON(`{"title":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if err := gdb.Exec("UPDATE notifications SET read_at = now() WHERE id = ?", alreadyReadForUser1.ID).Error; err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if _, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: notification.ChannelInApp, Recipient: datatypes.JSON(`{"user_id":"user-2"}`), Content: datatypes.JSON(`{"title":"hi"}`),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	// Unread on a non-InApp channel for the same user must not be returned -
	// only InApp has a pull-based inbox.
	if _, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel: notification.ChannelEmail, Recipient: datatypes.JSON(`{"user_id":"user-1","email":"a@example.com"}`), Content: datatypes.JSON(`{"subject":"hi"}`),
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	unread, err := svc.ConsumeUnreadInAppForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ConsumeUnreadInAppForUser: %v", err)
	}
	if len(unread) != 1 || unread[0].ID != unreadForUser1.ID {
		t.Fatalf("unread = %+v", unread)
	}

	// A second call should see nothing left unread - the first call marked
	// what it returned as read.
	again, err := svc.ConsumeUnreadInAppForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ConsumeUnreadInAppForUser (second call): %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second call = %+v, want none left unread", again)
	}
}
