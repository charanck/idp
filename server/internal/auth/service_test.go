package auth_test

import (
	"testing"

	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/testutil"
)

func setup(t *testing.T) (*gorm.DB, *auth.AuthService) {
	t.Helper()
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	return gdb, auth.NewAuthService(gdb)
}

func TestRegisterUser_CreatesInactiveUser(t *testing.T) {
	_, svc := setup(t)

	user, err := svc.RegisterUser("alice@example.com", "hunter22222", "")
	if err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}
	if user.IsActive {
		t.Fatal("expected new user to be inactive until admin activation")
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("email = %q", user.Email)
	}
	if user.Username == "" {
		t.Fatal("expected an auto-generated username")
	}
}

func TestRegisterUser_RejectsDuplicateEmail(t *testing.T) {
	_, svc := setup(t)

	if _, err := svc.RegisterUser("bob@example.com", "pw123456", ""); err != nil {
		t.Fatalf("first RegisterUser: %v", err)
	}
	if _, err := svc.RegisterUser("bob@example.com", "different-pw", ""); err == nil {
		t.Fatal("expected error for duplicate email")
	}
}

func TestAuthenticateUser_RequiresActiveAndCorrectPassword(t *testing.T) {
	gdb, svc := setup(t)

	user, err := svc.RegisterUser("carol@example.com", "correct-password", "")
	if err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}

	// Inactive users can't authenticate yet.
	got, err := svc.AuthenticateUser("carol@example.com", "correct-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for inactive user")
	}

	// Activate, then re-check.
	if err := gdb.Model(&auth.User{}).Where("id = ?", user.ID).Update("is_active", true).Error; err != nil {
		t.Fatalf("activate user: %v", err)
	}

	got, err = svc.AuthenticateUser("carol@example.com", "wrong-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for wrong password")
	}

	got, err = svc.AuthenticateUser("carol@example.com", "correct-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if got == nil || got.Email != "carol@example.com" {
		t.Fatalf("expected authenticated user, got %+v", got)
	}
}

func TestCreateServiceClient_AndAuthenticateAPIKey(t *testing.T) {
	_, svc := setup(t)

	creds, err := svc.CreateServiceClient("payments-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	if creds.Client.EncryptionKey == "" {
		t.Fatal("expected a client encryption key to be generated")
	}

	client, err := svc.AuthenticateServiceAPIKey(creds.APIKey)
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client == nil || client.Name != "payments-service" {
		t.Fatalf("expected authenticated client, got %+v", client)
	}

	client, err = svc.AuthenticateServiceAPIKey(*creds.Client.APIKeyID + ".wrong-secret")
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client != nil {
		t.Fatal("expected nil for wrong secret")
	}

	client, err = svc.AuthenticateServiceAPIKey("malformed-no-dot")
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client != nil {
		t.Fatal("expected nil for malformed key")
	}
}

func TestCreateServiceClient_RejectsDuplicateName(t *testing.T) {
	_, svc := setup(t)

	if _, err := svc.CreateServiceClient("dup-service"); err != nil {
		t.Fatalf("first CreateServiceClient: %v", err)
	}
	if _, err := svc.CreateServiceClient("dup-service"); err == nil {
		t.Fatal("expected error for duplicate service client name")
	}
}
