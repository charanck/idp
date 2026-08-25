package notification_test

import (
	"context"
	"testing"

	"gorm.io/datatypes"

	"controlplane/internal/crypto"
	"controlplane/internal/notification"
	"controlplane/internal/testutil"
)

func newTestProviderSettingService(t *testing.T) *notification.ProviderSettingService {
	t.Helper()
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)

	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return notification.NewProviderSettingService(notification.NewProviderSettingRepository(gdb), crypto.NewEncryptionService(masterKey))
}

func TestProviderSettingUpsert_CreatesThenUpdates(t *testing.T) {
	svc := newTestProviderSettingService(t)

	created, err := svc.Upsert(context.Background(), notification.UpsertInput{
		Channel:     notification.ChannelEmail,
		Config:      datatypes.JSON(`{"from":"noreply@example.com"}`),
		Credentials: "super-secret-api-key",
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("Upsert (create): %v", err)
	}
	if created.Credentials == "" || created.Credentials == "super-secret-api-key" {
		t.Fatalf("credentials not encrypted: %q", created.Credentials)
	}

	decrypted, err := svc.DecryptCredentials(created)
	if err != nil {
		t.Fatalf("DecryptCredentials: %v", err)
	}
	if decrypted != "super-secret-api-key" {
		t.Fatalf("decrypted = %q", decrypted)
	}

	updated, err := svc.Upsert(context.Background(), notification.UpsertInput{
		Channel:  notification.ChannelEmail,
		Config:   datatypes.JSON(`{"from":"updated@example.com"}`),
		IsActive: false,
		// Credentials left blank - should leave the existing value unchanged.
	})
	if err != nil {
		t.Fatalf("Upsert (update): %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("update created a new row: %s != %s", updated.ID, created.ID)
	}
	if updated.IsActive {
		t.Fatalf("IsActive not updated")
	}
	stillDecrypted, err := svc.DecryptCredentials(updated)
	if err != nil {
		t.Fatalf("DecryptCredentials after blank-credentials update: %v", err)
	}
	if stillDecrypted != "super-secret-api-key" {
		t.Fatalf("credentials changed on blank update: %q", stillDecrypted)
	}
}

func TestProviderSettingGet_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := newTestProviderSettingService(t)

	got, err := svc.Get(context.Background(), notification.ChannelWhatsApp)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestProviderSettingList_ReturnsOnlyConfiguredChannels(t *testing.T) {
	svc := newTestProviderSettingService(t)

	if _, err := svc.Upsert(context.Background(), notification.UpsertInput{Channel: notification.ChannelSMS, Config: datatypes.JSON(`{}`), Credentials: "k", IsActive: true}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	settings, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(settings) != 1 || settings[0].Channel != notification.ChannelSMS {
		t.Fatalf("settings = %+v", settings)
	}
}
