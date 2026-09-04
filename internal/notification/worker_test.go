package notification_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	_ "github.com/dbos-inc/dbos-transact-golang/dbos/driver/sqlite"
	"github.com/google/uuid"
	"gorm.io/datatypes"

	"controlplane/internal/crypto"
	model "controlplane/internal/model/notification"
	"controlplane/internal/notification"
	"controlplane/internal/notification/provider"
)

// newTestDBOSContext returns a DBOS context backed by a throwaway file-based
// SQLite system database (not sqlite::memory: - that opens a fresh empty
// database per pooled connection, so migrated tables vanish under
// concurrent access) - enough to run SendWorkflow's dbos.RunAsStep call
// (which requires an actual DBOS workflow execution, not just any
// dbos.Context) in a unit test without a real Postgres instance.
func newTestDBOSContext(t *testing.T) dbos.Context {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "dbos-test.db")
	ctx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:     "notification-test",
		DatabaseURL: "sqlite:" + dbPath,
	})
	if err != nil {
		t.Fatalf("dbos.NewContext: %v", err)
	}
	t.Cleanup(func() {
		_ = dbos.Shutdown(ctx, 5*time.Second)
	})
	return ctx
}

func newUnitWorker(t *testing.T, channels notification.ChannelRegistry, hub notification.Publisher) (*notification.Worker, *fakeNotificationRepository, *notification.NotificationService, *notification.ProviderSettingService) {
	t.Helper()
	repo := newFakeNotificationRepository()
	enqueuer := &fakeEnqueuer{}
	notifications := notification.NewNotificationService(repo, newFakeApplicationRepository(), enqueuer)

	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	settings := notification.NewProviderSettingService(newFakeProviderSettingRepository(), crypto.NewEncryptionService(masterKey))

	return notification.NewWorker(notifications, settings, channels, hub), repo, notifications, settings
}

// handleSend runs w.SendWorkflow for id as a real (SQLite-backed) DBOS
// workflow execution, mirroring how TaskEnqueuer runs it in production, and
// waits for it to complete.
func handleSend(t *testing.T, w *notification.Worker, id string) error {
	t.Helper()
	notificationID, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("uuid.Parse: %v", err)
	}

	ctx := newTestDBOSContext(t)
	dbos.RegisterWorkflow(ctx, w.SendWorkflow)
	if err := dbos.Launch(ctx); err != nil {
		t.Fatalf("dbos.Launch: %v", err)
	}

	handle, err := dbos.RunWorkflow(ctx, w.SendWorkflow, notification.SendPayload{NotificationID: notificationID},
		dbos.WithWorkflowID(id))
	if err != nil {
		return err
	}
	_, err = handle.GetResult()
	return err
}

func TestWorker_InAppSendPublishesToHub(t *testing.T) {
	channel := &fakeChannel{result: &provider.Result{Provider: "inapp", ProviderMessageID: "m1"}}
	hub := &fakeHub{}
	w, repo, notifications, _ := newUnitWorker(t, notification.ChannelRegistry{model.ChannelInApp: channel}, hub)
	ctx := context.Background()

	n, err := notifications.CreateNotification(ctx, notification.CreateNotificationInput{
		Channel:   model.ChannelInApp,
		Recipient: datatypes.JSON(`{"user_id":"u1"}`),
		Content:   datatypes.JSON(`{"title":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	if err := handleSend(t, w, n.ID.String()); err != nil {
		t.Fatalf("HandleSend: %v", err)
	}

	if hub.count() != 1 {
		t.Fatalf("expected 1 SSE publish for inapp send, got %d", hub.count())
	}

	got, err := repo.FindByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.StatusSent {
		t.Fatalf("status = %q, want sent", got.Status)
	}
}

func TestWorker_NonInAppSentNeverPublishes(t *testing.T) {
	for _, channelName := range []string{model.ChannelEmail, model.ChannelSMS} {
		t.Run(channelName, func(t *testing.T) {
			channel := &fakeChannel{result: &provider.Result{Provider: channelName, ProviderMessageID: "m1"}}
			hub := &fakeHub{}
			w, _, notifications, _ := newUnitWorker(t, notification.ChannelRegistry{channelName: channel}, hub)
			ctx := context.Background()

			n, err := notifications.CreateNotification(ctx, notification.CreateNotificationInput{
				Channel:   channelName,
				Recipient: datatypes.JSON(`{}`),
				Content:   datatypes.JSON(`{}`),
			})
			if err != nil {
				t.Fatalf("CreateNotification: %v", err)
			}

			if err := handleSend(t, w, n.ID.String()); err != nil {
				t.Fatalf("HandleSend: %v", err)
			}

			if hub.count() != 0 {
				t.Fatalf("expected 0 SSE publishes for %s send, got %d", channelName, hub.count())
			}
		})
	}
}

func TestWorker_NonInAppFailureNeverPublishes(t *testing.T) {
	channel := &fakeChannel{err: &provider.SendError{Transient: false, Err: errors.New("permanent failure")}}
	hub := &fakeHub{}
	w, repo, notifications, _ := newUnitWorker(t, notification.ChannelRegistry{model.ChannelEmail: channel}, hub)
	ctx := context.Background()

	n, err := notifications.CreateNotification(ctx, notification.CreateNotificationInput{
		Channel:   model.ChannelEmail,
		Recipient: datatypes.JSON(`{}`),
		Content:   datatypes.JSON(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	if err := handleSend(t, w, n.ID.String()); err == nil {
		t.Fatal("expected HandleSend to return an error for a permanent send failure")
	}

	if hub.count() != 0 {
		t.Fatalf("expected 0 SSE publishes for a failed email send, got %d", hub.count())
	}

	got, err := repo.FindByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != model.StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

func TestWorker_LoadsAndDecryptsSettingsForSend(t *testing.T) {
	channel := &fakeChannel{result: &provider.Result{Provider: "smtp", ProviderMessageID: "m1"}}
	w, _, notifications, settingsSvc := newUnitWorker(t, notification.ChannelRegistry{model.ChannelEmail: channel}, nil)
	ctx := context.Background()

	_, err := settingsSvc.Upsert(ctx, notification.UpsertInput{
		Channel:     model.ChannelEmail,
		Config:      datatypes.JSON(`{"provider":"smtp","host":"smtp.example.com","port":587,"from":"noreply@example.com","tls_mode":"none"}`),
		Credentials: `{"username":"u","password":"p"}`,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	n, err := notifications.CreateNotification(ctx, notification.CreateNotificationInput{
		Channel:   model.ChannelEmail,
		Recipient: datatypes.JSON(`{"email":"a@example.com"}`),
		Content:   datatypes.JSON(`{"subject":"hi"}`),
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	if err := handleSend(t, w, n.ID.String()); err != nil {
		t.Fatalf("HandleSend: %v", err)
	}

	if !channel.sawSettings {
		t.Fatal("expected channel.Send to be called")
	}
	if string(channel.lastSettings.Config) != `{"provider":"smtp","host":"smtp.example.com","port":587,"from":"noreply@example.com","tls_mode":"none"}` {
		t.Fatalf("Config = %s", channel.lastSettings.Config)
	}
	if channel.lastSettings.Credentials != `{"username":"u","password":"p"}` {
		t.Fatalf("Credentials = %s, want decrypted plaintext", channel.lastSettings.Credentials)
	}
}
