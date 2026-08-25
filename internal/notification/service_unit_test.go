package notification_test

import (
	"context"
	"testing"

	"gorm.io/datatypes"

	"controlplane/internal/notification"
)

func TestCreateNotification_IdempotentQueuedReEnqueues(t *testing.T) {
	repo := newFakeNotificationRepository()
	enqueuer := &fakeEnqueuer{}
	svc := notification.NewNotificationService(repo, enqueuer)
	ctx := context.Background()

	in := notification.CreateNotificationInput{
		Channel:        notification.ChannelInApp,
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
	if enqueuer.count() != 2 {
		t.Fatalf("expected re-enqueue on 2nd create of still-queued notification, got %d enqueue calls", enqueuer.count())
	}
}

func TestConsumeUnreadInAppForUser_OnlyInAppChannelAndMarksRead(t *testing.T) {
	repo := newFakeNotificationRepository()
	svc := notification.NewNotificationService(repo, &fakeEnqueuer{})
	ctx := context.Background()

	mustCreate := func(channel, userID string) *notification.Notification {
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

	inApp := mustCreate(notification.ChannelInApp, "u1")
	mustCreate(notification.ChannelEmail, "u1")
	mustCreate(notification.ChannelInApp, "u2")

	unread, err := svc.ConsumeUnreadInAppForUser(ctx, "u1")
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

	againUnread, err := svc.ConsumeUnreadInAppForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ConsumeUnreadInAppForUser (2nd): %v", err)
	}
	if len(againUnread) != 0 {
		t.Fatalf("expected 0 unread on 2nd call, got %d", len(againUnread))
	}
}
