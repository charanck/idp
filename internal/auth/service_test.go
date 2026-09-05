package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/security"
)

func newTestAuthService() (*auth.AuthService, *fakeUserRepository, *fakeServiceClientRepository) {
	users := newFakeUserRepository()
	clients := newFakeServiceClientRepository()
	groups := newFakeGroupRepository()
	policies := newFakePolicyRepository()
	return auth.NewAuthService(users, clients, groups, policies), users, clients
}

// newTestAuthServiceWithGroups is newTestAuthService plus direct access to
// the group/policy fakes, for tests exercising group- and policy-dependent
// behavior (default group assignment, self-registration domain policy).
func newTestAuthServiceWithGroups() (*auth.AuthService, *fakeGroupRepository, *fakePolicyRepository) {
	users := newFakeUserRepository()
	clients := newFakeServiceClientRepository()
	groups := newFakeGroupRepository()
	policies := newFakePolicyRepository()
	return auth.NewAuthService(users, clients, groups, policies), groups, policies
}

// legacyPBKDF2Hash builds a hash in the pre-argon2id "pbkdf2_sha256"
// format, for tests exercising the rehash-on-successful-auth shim.
func legacyPBKDF2Hash(password string) string {
	salt := "test-legacy-salt"
	iterations := 100_000
	hash := pbkdf2.Key([]byte(password), []byte(salt), iterations, 32, sha256.New)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", iterations, salt, base64.StdEncoding.EncodeToString(hash))
}

func TestRegisterUser_CreatesInactiveUser(t *testing.T) {
	svc, _, _ := newTestAuthService()

	user, err := svc.RegisterUser(context.Background(), "alice@example.com", "hunter22222", "")
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

func TestRegisterUser_DomainPolicyRejectsDisallowedDomain(t *testing.T) {
	svc, _, _ := newTestAuthServiceWithGroups()
	ctx := context.Background()

	if _, err := svc.UpdatePolicy(ctx, "example.com, other.com"); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}

	if _, err := svc.RegisterUser(ctx, "alice@example.com", "hunter22222", ""); err != nil {
		t.Fatalf("RegisterUser with allowed domain: %v", err)
	}

	_, err := svc.RegisterUser(ctx, "mallory@evil.com", "hunter22222", "")
	if !errors.Is(err, auth.ErrDomainNotAllowed) {
		t.Fatalf("expected ErrDomainNotAllowed, got %v", err)
	}
}

func TestRegisterUser_AssignsDefaultUserGroup(t *testing.T) {
	svc, groups, _ := newTestAuthServiceWithGroups()
	ctx := context.Background()

	userGroup := authmodel.Group{ID: uuid.New(), Name: "User", IsSystem: true}
	if err := groups.Create(ctx, &userGroup); err != nil {
		t.Fatalf("seed User group: %v", err)
	}

	user, err := svc.RegisterUser(ctx, "alice@example.com", "hunter22222", "")
	if err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}

	assigned, err := groups.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(assigned) != 1 || assigned[0].ID != userGroup.ID {
		t.Fatalf("expected new user assigned to built-in User group, got %+v", assigned)
	}
}

func TestRegisterUser_DuplicateEmailReturnsErrAlreadyExists(t *testing.T) {
	svc, _, _ := newTestAuthService()
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
	svc, users, _ := newTestAuthService()
	ctx := context.Background()

	hashed, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := users.Create(ctx, &authmodel.User{Email: "user@example.com", IsActive: true, Password: hashed}); err != nil {
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
	svc, users, _ := newTestAuthService()
	ctx := context.Background()

	hashed, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := users.Create(ctx, &authmodel.User{Email: "user@example.com", IsActive: false, Password: hashed}); err != nil {
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

func TestAuthenticateUser_ReturnsUserForCorrectCredentials(t *testing.T) {
	svc, users, _ := newTestAuthService()
	ctx := context.Background()

	hashed, err := security.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := users.Create(ctx, &authmodel.User{Email: "carol@example.com", IsActive: true, Password: hashed}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	user, err := svc.AuthenticateUser(ctx, "carol@example.com", "correct-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user == nil || user.Email != "carol@example.com" {
		t.Fatalf("expected authenticated user, got %+v", user)
	}
}

func TestAuthenticateUser_RehashesLegacyPasswordOnSuccess(t *testing.T) {
	svc, users, _ := newTestAuthService()
	ctx := context.Background()

	legacyHash := legacyPBKDF2Hash("correct-password")
	if err := users.Create(ctx, &authmodel.User{Email: "dana@example.com", IsActive: true, Password: legacyHash}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	user, err := svc.AuthenticateUser(ctx, "dana@example.com", "correct-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if user == nil {
		t.Fatal("expected authenticated user")
	}

	updated, err := users.FindByEmail(ctx, "dana@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if security.IsLegacyHash(updated.Password) {
		t.Fatalf("expected password to be rehashed to argon2id, still legacy: %q", updated.Password)
	}
	if !security.VerifyPassword("correct-password", updated.Password) {
		t.Fatal("expected rehashed password to still verify")
	}
}

func TestCreateServiceClient_DuplicateNameReturnsErrAlreadyExists(t *testing.T) {
	svc, _, _ := newTestAuthService()
	ctx := context.Background()

	if _, err := svc.CreateServiceClient(ctx, "billing-service"); err != nil {
		t.Fatalf("first CreateServiceClient: %v", err)
	}
	_, err := svc.CreateServiceClient(ctx, "billing-service")
	if !errors.Is(err, auth.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestCreateServiceClient_GeneratesEncryptionKeyAndAuthenticatesAPIKey(t *testing.T) {
	svc, _, _ := newTestAuthService()
	ctx := context.Background()

	creds, err := svc.CreateServiceClient(ctx, "payments-service")
	if err != nil {
		t.Fatalf("CreateServiceClient: %v", err)
	}
	if creds.Client.EncryptionKey == "" {
		t.Fatal("expected a client encryption key to be generated")
	}

	client, err := svc.AuthenticateServiceAPIKey(ctx, creds.APIKey)
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client == nil || client.Name != "payments-service" {
		t.Fatalf("expected authenticated client, got %+v", client)
	}
}

func TestAuthenticateServiceAPIKey_RehashesLegacySecretOnSuccess(t *testing.T) {
	svc, _, clients := newTestAuthService()
	ctx := context.Background()

	keyID := "sk_live_legacytest"
	legacyHash := legacyPBKDF2Hash("legacy-secret")
	if err := clients.Create(ctx, &authmodel.ServiceClient{
		Name:          "legacy-service",
		APIKeyID:      &keyID,
		APIKeyHash:    legacyHash,
		EncryptionKey: "unused",
		IsActive:      true,
	}); err != nil {
		t.Fatalf("seed client: %v", err)
	}

	client, err := svc.AuthenticateServiceAPIKey(ctx, keyID+".legacy-secret")
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client == nil {
		t.Fatal("expected authenticated client")
	}

	updated, err := clients.FindByAPIKeyIDActive(ctx, keyID)
	if err != nil {
		t.Fatalf("FindByAPIKeyIDActive: %v", err)
	}
	if security.IsLegacyHash(updated.APIKeyHash) {
		t.Fatalf("expected api key hash to be rehashed to argon2id, still legacy: %q", updated.APIKeyHash)
	}
	if !security.VerifyPassword("legacy-secret", updated.APIKeyHash) {
		t.Fatal("expected rehashed api key secret to still verify")
	}
}

func TestAuthenticateServiceAPIKey_WrongSecretReturnsNil(t *testing.T) {
	svc, _, _ := newTestAuthService()
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

func TestAuthenticateServiceAPIKey_MalformedKeyReturnsNil(t *testing.T) {
	svc, _, _ := newTestAuthService()
	ctx := context.Background()

	client, err := svc.AuthenticateServiceAPIKey(ctx, "malformed-no-dot")
	if err != nil {
		t.Fatalf("AuthenticateServiceAPIKey: %v", err)
	}
	if client != nil {
		t.Fatalf("expected nil client for malformed key, got %+v", client)
	}
}

func TestAuthenticateServiceAPIKey_InactiveClientReturnsNil(t *testing.T) {
	svc, _, clients := newTestAuthService()
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
	svc, users, _ := newTestAuthService()
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
	if err := users.Create(context.Background(), &authmodel.User{Email: email, IsStaff: isStaff, IsActive: true}); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
}
