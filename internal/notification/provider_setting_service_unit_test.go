package notification_test

import (
	"context"
	"testing"

	"gorm.io/datatypes"

	"controlplane/internal/crypto"
	"controlplane/internal/notification"
)

func newUnitProviderSettingService(t *testing.T) (*notification.ProviderSettingService, *fakeProviderSettingRepository) {
	t.Helper()
	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	repo := newFakeProviderSettingRepository()
	svc := notification.NewProviderSettingService(repo, crypto.NewEncryptionService(masterKey))
	return svc, repo
}

func TestUpsert_PreservesExistingCredentialsWhenBlank(t *testing.T) {
	svc, _ := newUnitProviderSettingService(t)
	ctx := context.Background()

	created, err := svc.Upsert(ctx, notification.UpsertInput{
		Channel:     notification.ChannelEmail,
		Config:      datatypes.JSON(`{"from":"noreply@example.com"}`),
		Credentials: "super-secret",
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("Upsert (create): %v", err)
	}
	if created.Credentials == "" || created.Credentials == "super-secret" {
		t.Fatalf("expected credentials to be encrypted, got %q", created.Credentials)
	}

	updated, err := svc.Upsert(ctx, notification.UpsertInput{
		Channel:  notification.ChannelEmail,
		Config:   datatypes.JSON(`{"from":"updated@example.com"}`),
		IsActive: false,
	})
	if err != nil {
		t.Fatalf("Upsert (update, blank credentials): %v", err)
	}
	if updated.Credentials != created.Credentials {
		t.Fatalf("expected credentials to be preserved on blank update, got %q want %q", updated.Credentials, created.Credentials)
	}
	if updated.IsActive {
		t.Fatal("expected IsActive to be updated to false")
	}

	decrypted, err := svc.DecryptCredentials(updated)
	if err != nil {
		t.Fatalf("DecryptCredentials: %v", err)
	}
	if decrypted != "super-secret" {
		t.Fatalf("decrypted credentials = %q, want super-secret", decrypted)
	}
}
