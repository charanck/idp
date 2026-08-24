package notification_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"

	"controlplane/internal/notification"
	"controlplane/internal/testutil"
)

// TestNotificationDelivery_CreateEnqueueProcessSent drives a notification
// through the full create -> enqueue -> asynq process -> sent pipeline
// against a real asynq client/server pair (backed by miniredis) and the
// skeleton email channel.
func TestNotificationDelivery_CreateEnqueueProcessSent(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	client := notification.NewAsynqClient(rdb)
	t.Cleanup(func() { client.Close() })
	enqueuer := notification.NewTaskEnqueuer(client)

	svc := notification.NewNotificationService(gdb, enqueuer)
	hub := notification.NewHub(rdb)
	worker := notification.NewWorker(svc, nil, notification.NewChannelRegistry(), hub)

	server := notification.NewAsynqServer(rdb, asynq.Config{Concurrency: 1, LogLevel: asynq.FatalLevel})
	mux := notification.NewAsynqMux(worker)
	if err := server.Start(mux); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	t.Cleanup(server.Shutdown)

	n, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel:   notification.ChannelEmail,
		Recipient: datatypes.JSON(`{"email":"a@example.com"}`),
		Content:   datatypes.JSON(`{"subject":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := svc.GetNotification(context.Background(), n.ID)
		if err != nil {
			t.Fatalf("GetNotification: %v", err)
		}
		if got.Status == notification.StatusSent {
			if got.ProviderMessageID == nil || *got.ProviderMessageID == "" {
				t.Fatalf("sent notification missing ProviderMessageID: %+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for status=sent, last status = %q", got.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestNotificationDelivery_SSEPublishReflectsFinalStatus drives the same
// pipeline as above but for a recipient carrying a "user_id" field, and
// asserts the Hub publishes a matching fire-and-forget SSE event once the
// notification reaches its terminal "sent" status.
func TestNotificationDelivery_SSEPublishReflectsFinalStatus(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	client := notification.NewAsynqClient(rdb)
	t.Cleanup(func() { client.Close() })
	enqueuer := notification.NewTaskEnqueuer(client)

	svc := notification.NewNotificationService(gdb, enqueuer)
	hub := notification.NewHub(rdb)
	worker := notification.NewWorker(svc, nil, notification.NewChannelRegistry(), hub)

	server := notification.NewAsynqServer(rdb, asynq.Config{Concurrency: 1, LogLevel: asynq.FatalLevel})
	mux := notification.NewAsynqMux(worker)
	if err := server.Start(mux); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	t.Cleanup(server.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := hub.Subscribe(ctx, "user-1")
	t.Cleanup(func() { sub.Close() })

	n, err := svc.CreateNotification(context.Background(), notification.CreateNotificationInput{
		Channel:   notification.ChannelEmail,
		Recipient: datatypes.JSON(`{"email":"a@example.com","user_id":"user-1"}`),
		Content:   datatypes.JSON(`{"subject":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	select {
	case msg := <-sub.Channel():
		if !strings.Contains(msg.Payload, n.ID.String()) || !strings.Contains(msg.Payload, notification.StatusSent) {
			t.Fatalf("unexpected sse payload: %s", msg.Payload)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for sse publish")
	}
}
