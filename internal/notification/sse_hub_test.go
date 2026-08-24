package notification_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"

	"controlplane/internal/notification"
)

func newTestHub(t *testing.T) *notification.Hub {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return notification.NewHub(rdb)
}

func TestHub_PublishSentDeliversToSubscriber(t *testing.T) {
	hub := newTestHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pubsub := hub.Subscribe(ctx, "user-1")
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("Receive (subscribe confirmation): %v", err)
	}

	providerStr := "email-sim"
	n := &notification.Notification{
		ID:        uuid.New(),
		Channel:   notification.ChannelEmail,
		Recipient: datatypes.JSON(`{"user_id":"user-1"}`),
		Status:    notification.StatusSent,
		Provider:  &providerStr,
	}
	if err := hub.PublishSent(ctx, n); err != nil {
		t.Fatalf("PublishSent: %v", err)
	}

	select {
	case msg := <-pubsub.Channel():
		var event notification.SentEvent
		if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if event.ID != n.ID.String() || event.Status != notification.StatusSent {
			t.Fatalf("event = %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for published event")
	}
}

func TestHub_PublishSentNoOpWithoutRecipientUserID(t *testing.T) {
	hub := newTestHub(t)
	ctx := context.Background()

	n := &notification.Notification{
		ID:        uuid.New(),
		Channel:   notification.ChannelEmail,
		Recipient: datatypes.JSON(`{"email":"a@example.com"}`),
		Status:    notification.StatusSent,
	}
	if err := hub.PublishSent(ctx, n); err != nil {
		t.Fatalf("PublishSent should no-op without user_id, got err: %v", err)
	}
}
