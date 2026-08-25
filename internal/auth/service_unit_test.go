package auth_test

import (
	"context"
	"errors"
	"testing"

	"controlplane/internal/auth"
	"controlplane/internal/security"
)

func newUnitAuthService() (*auth.AuthService, *fakeUserRepository, *fakeServiceClientRepository) {
	users := newFakeUserRepository()
	clients := newFakeServiceClientRepository()
	return auth.NewAuthService(users, clients), users, clients
}

func TestRegisterUser_DuplicateEmailReturnsErrAlreadyExists(t *testing.T) {
	svc, _, _ := newUnitAuthService()
	ctx := context.Background()

	if _, err := svc.RegisterUser(ctx, "dup@example.com", "password123", ""); err != nil {
		t.Fatalf("first RegisterUser: %v", err)
	}
	_, err := svc.RegisterUser(ctx, "dup@example.com", "password123", "")
	if !errors.Is(err, auth.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestAuthenticateUser_WrongPasswordReturnsNil(t *testing.T) {
	svc, users, _ := newUnitAuthService()
	ctx := context.Background()

	hashed, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := users.Create(ctx, &auth.User{Email: "user@example.com", IsActive: true, Password: hashed}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	user, err := svc.AuthenticateUser(ctx, "user@example.com", "wrong-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user != nil {
		t.Fatalf("expected nil user for wrong password, got %+v", user)
	}
}

func TestAuthenticateUser_InactiveUserReturnsNil(t *testing.T) {
	svc, users, _ := newUnitAuthService()
	ctx := context.Background()

	hashed, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := users.Create(ctx, &auth.User{Email: "user@example.com", IsActive: false, Password: hashed}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	user, err := svc.AuthenticateUser(ctx, "user@example.com", "correct-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user != nil {
		t.Fatalf("expected nil user for inactive account, got %+v", user)
	}
}

func TestCreateServiceClient_DuplicateNameReturnsErrAlreadyExists(t *testing.T) {
	svc, _, _ := newUnitAuthService()
	ctx := context.Background()

	if _, err := svc.CreateServiceClient(ctx, "billing-service"); err != nil {
		t.Fatalf("first CreateServiceClient: %v", err)
	}
	_, err := svc.CreateServiceClient(ctx, "billing-service")
	if !errors.Is(err, auth.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestAuthenticateServiceAPIKey_WrongSecretReturnsNil(t *testing.T) {
	svc, _, _ := newUnitAuthService()
	ctx := context.Background()

	creds, err := svc.CreateServiceClient(ctx, "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}

	client, err := svc.AuthenticateServiceAPIKey(ctx, *creds.Client.APIKeyID+".wrong-secret")
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client != nil {
		t.Fatalf("expected nil client for wrong secret, got %+v", client)
	}
}

func TestAuthenticateServiceAPIKey_InactiveClientReturnsNil(t *testing.T) {
	svc, _, clients := newUnitAuthService()
	ctx := context.Background()

	creds, err := svc.CreateServiceClient(ctx, "billing-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	creds.Client.IsActive = false
	if err := clients.Update(ctx, creds.Client); err != nil {
		t.Fatalf("deactivate client: %v", err)
	}

	client, err := svc.AuthenticateServiceAPIKey(ctx, creds.APIKey)
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client != nil {
		t.Fatalf("expected nil client for inactive client, got %+v", client)
	}
}

func TestListUsers_FiltersByQueryAndStaff(t *testing.T) {
	svc, users, _ := newUnitAuthService()
	ctx := context.Background()

	mustCreateUser(t, users, "alice@example.com", true)
	mustCreateUser(t, users, "bob@example.com", false)
	mustCreateUser(t, users, "alice2@other.com", false)

	staff := true
	results, err := svc.ListUsers(ctx, "alice", &staff)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(results) != 1 || results[0].Email != "alice@example.com" {
		t.Fatalf("expected exactly alice@example.com, got %+v", results)
	}
}

func mustCreateUser(t *testing.T, users *fakeUserRepository, email string, isStaff bool) {
	t.Helper()
	if err := users.Create(context.Background(), &auth.User{Email: email, IsStaff: isStaff, IsActive: true}); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
}
