package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"controlplane/internal/auth"
	"controlplane/internal/crypto"
	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/security"
)

const testRedirectURI = "https://app.example.com/callback"

type oidcTestFixture struct {
	svc       *auth.OIDCService
	users     *fakeUserRepository
	clients   *fakeServiceClientRepository
	groups    *fakeGroupRepository
	authCodes *fakeOIDCAuthorizationCodeRepository
}

func newOIDCTestFixture(t *testing.T) *oidcTestFixture {
	t.Helper()
	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	users := newFakeUserRepository()
	clients := newFakeServiceClientRepository()
	groups := newFakeGroupRepository()
	signingKeys := newFakeOIDCSigningKeyRepository()
	authCodes := newFakeOIDCAuthorizationCodeRepository()

	svc := auth.NewOIDCService(signingKeys, authCodes, clients, groups, users, crypto.NewEncryptionService(masterKey))
	return &oidcTestFixture{svc: svc, users: users, clients: clients, groups: groups, authCodes: authCodes}
}

// createAuthApplication seeds an active ServiceClient enabled as an OIDC auth
// application with testRedirectURI registered, returning the client, its
// client_id, and its plaintext client_secret.
func (f *oidcTestFixture) createAuthApplication(t *testing.T, allowedGroupIDs []uuid.UUID) (client *authmodel.ServiceClient, clientID, clientSecret string) {
	t.Helper()
	ctx := context.Background()

	clientID = "client_" + uuid.NewString()
	clientSecret = "s3cr3t-" + uuid.NewString()
	hashed, err := security.HashPassword(clientSecret)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	client = &authmodel.ServiceClient{
		ID:                uuid.New(),
		Name:              "test-app",
		APIKeyID:          &clientID,
		APIKeyHash:        hashed,
		IsActive:          true,
		IsAuthApplication: true,
	}
	if err := f.clients.Create(ctx, client); err != nil {
		t.Fatalf("create service client: %v", err)
	}
	if err := f.clients.SetRedirectURIs(ctx, client.ID, []string{testRedirectURI}); err != nil {
		t.Fatalf("SetRedirectURIs: %v", err)
	}
	if err := f.clients.SetAllowedGroups(ctx, client.ID, allowedGroupIDs); err != nil {
		t.Fatalf("SetAllowedGroups: %v", err)
	}
	return client, clientID, clientSecret
}

func (f *oidcTestFixture) createUser(t *testing.T) *authmodel.User {
	t.Helper()
	user := &authmodel.User{ID: uuid.New(), Email: "alice@example.com", Username: "alice", IsActive: true}
	if err := f.users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func TestOIDCService_IssueAndExchangeCode_Succeeds(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	client, clientID, clientSecret := f.createAuthApplication(t, nil)
	user := f.createUser(t)

	validated, err := f.svc.ValidateClient(ctx, clientID, testRedirectURI)
	if err != nil {
		t.Fatalf("ValidateClient: %v", err)
	}
	if validated.ID != client.ID {
		t.Fatalf("ValidateClient returned wrong client")
	}

	authCode, err := f.svc.IssueAuthorizationCode(ctx, client, user.ID, testRedirectURI, "openid profile email groups", "test-nonce")
	if err != nil {
		t.Fatalf("IssueAuthorizationCode: %v", err)
	}

	idToken, accessToken, expiresIn, err := f.svc.ExchangeCode(ctx, clientID, clientSecret, authCode.Code, testRedirectURI, "https://idp.example.com")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if idToken == "" || accessToken == "" {
		t.Fatal("expected non-empty tokens")
	}
	if expiresIn <= 0 {
		t.Fatalf("expected positive expires_in, got %d", expiresIn)
	}

	jwks, err := f.svc.JWKS(ctx)
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	keys := jwks["keys"].([]map[string]any)
	if len(keys) != 1 {
		t.Fatalf("expected 1 JWKS key, got %d", len(keys))
	}

	claims, err := f.svc.ValidateAccessToken(ctx, idToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken(idToken): %v", err)
	}
	if claims["sub"] != user.ID.String() {
		t.Fatalf("expected sub=%s, got %v", user.ID, claims["sub"])
	}
	if claims["nonce"] != "test-nonce" {
		t.Fatalf("expected nonce carried into ID token, got %v", claims["nonce"])
	}
	if claims["email"] != user.Email {
		t.Fatalf("expected email claim (scope requested), got %v", claims["email"])
	}
	if claims["preferred_username"] != user.Username {
		t.Fatalf("expected preferred_username claim, got %v", claims["preferred_username"])
	}

	if _, err := f.svc.ValidateAccessToken(ctx, accessToken); err != nil {
		t.Fatalf("ValidateAccessToken(accessToken): %v", err)
	}
}

func TestOIDCService_ExchangeCode_RejectsReplayedCode(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	client, clientID, clientSecret := f.createAuthApplication(t, nil)
	user := f.createUser(t)

	authCode, err := f.svc.IssueAuthorizationCode(ctx, client, user.ID, testRedirectURI, "openid", "")
	if err != nil {
		t.Fatalf("IssueAuthorizationCode: %v", err)
	}

	if _, _, _, err := f.svc.ExchangeCode(ctx, clientID, clientSecret, authCode.Code, testRedirectURI, "https://idp.example.com"); err != nil {
		t.Fatalf("first ExchangeCode: %v", err)
	}

	_, _, _, err = f.svc.ExchangeCode(ctx, clientID, clientSecret, authCode.Code, testRedirectURI, "https://idp.example.com")
	if err != auth.ErrOIDCCodeInvalid {
		t.Fatalf("expected ErrOIDCCodeInvalid on replay, got %v", err)
	}
}

func TestOIDCService_ExchangeCode_RejectsExpiredCode(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	client, clientID, clientSecret := f.createAuthApplication(t, nil)
	user := f.createUser(t)

	expiredCode := &authmodel.OIDCAuthorizationCode{
		Code:            uuid.NewString(),
		ServiceClientID: client.ID,
		UserID:          user.ID,
		RedirectURI:     testRedirectURI,
		Scope:           "openid",
		ExpiresAt:       time.Now().UTC().Add(-time.Minute),
	}
	if err := f.authCodes.Create(ctx, expiredCode); err != nil {
		t.Fatalf("seed expired code: %v", err)
	}

	_, _, _, err := f.svc.ExchangeCode(ctx, clientID, clientSecret, expiredCode.Code, testRedirectURI, "https://idp.example.com")
	if err != auth.ErrOIDCCodeInvalid {
		t.Fatalf("expected ErrOIDCCodeInvalid for expired code, got %v", err)
	}
}

func TestOIDCService_ExchangeCode_RejectsRedirectURIMismatch(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	client, clientID, clientSecret := f.createAuthApplication(t, nil)
	user := f.createUser(t)

	authCode, err := f.svc.IssueAuthorizationCode(ctx, client, user.ID, testRedirectURI, "openid", "")
	if err != nil {
		t.Fatalf("IssueAuthorizationCode: %v", err)
	}

	_, _, _, err = f.svc.ExchangeCode(ctx, clientID, clientSecret, authCode.Code, "https://attacker.example.com/callback", "https://idp.example.com")
	if err != auth.ErrOIDCCodeInvalid {
		t.Fatalf("expected ErrOIDCCodeInvalid for redirect_uri mismatch, got %v", err)
	}
}

func TestOIDCService_ValidateClient_RejectsRedirectURIMismatch(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	_, clientID, _ := f.createAuthApplication(t, nil)

	if _, err := f.svc.ValidateClient(ctx, clientID, "https://not-registered.example.com"); err != auth.ErrOIDCClientInvalid {
		t.Fatalf("expected ErrOIDCClientInvalid, got %v", err)
	}
}

func TestOIDCService_UserAllowedForClient_GroupRestricted(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	allowedGroup := authmodel.Group{ID: uuid.New(), Name: "Engineering"}
	if err := f.groups.Create(ctx, &allowedGroup); err != nil {
		t.Fatalf("seed group: %v", err)
	}

	client, _, _ := f.createAuthApplication(t, []uuid.UUID{allowedGroup.ID})
	user := f.createUser(t)

	allowed, err := f.svc.UserAllowedForClient(ctx, client, user.ID)
	if err != nil {
		t.Fatalf("UserAllowedForClient: %v", err)
	}
	if allowed {
		t.Fatal("expected user outside allowed groups to be rejected")
	}

	if err := f.groups.SetUserGroups(ctx, user.ID, []uuid.UUID{allowedGroup.ID}); err != nil {
		t.Fatalf("SetUserGroups: %v", err)
	}

	allowed, err = f.svc.UserAllowedForClient(ctx, client, user.ID)
	if err != nil {
		t.Fatalf("UserAllowedForClient: %v", err)
	}
	if !allowed {
		t.Fatal("expected user in allowed group to be permitted")
	}
}

func TestOIDCService_UserAllowedForClient_EmptyAllowListPermitsAnyUser(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	client, _, _ := f.createAuthApplication(t, nil)
	user := f.createUser(t)

	allowed, err := f.svc.UserAllowedForClient(ctx, client, user.ID)
	if err != nil {
		t.Fatalf("UserAllowedForClient: %v", err)
	}
	if !allowed {
		t.Fatal("expected empty allowed-groups list to permit any directory user")
	}
}

// Sanity check that ValidateAccessToken actually verifies the RS256
// signature rather than trusting any well-formed token.
func TestOIDCService_ValidateAccessToken_RejectsTamperedSignature(t *testing.T) {
	f := newOIDCTestFixture(t)
	ctx := context.Background()

	client, clientID, clientSecret := f.createAuthApplication(t, nil)
	user := f.createUser(t)

	authCode, err := f.svc.IssueAuthorizationCode(ctx, client, user.ID, testRedirectURI, "openid", "")
	if err != nil {
		t.Fatalf("IssueAuthorizationCode: %v", err)
	}
	idToken, _, _, err := f.svc.ExchangeCode(ctx, clientID, clientSecret, authCode.Code, testRedirectURI, "https://idp.example.com")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	tampered := idToken[:len(idToken)-1] + "x"
	if tampered == idToken {
		t.Fatal("test setup did not actually tamper the token")
	}
	if _, err := f.svc.ValidateAccessToken(ctx, tampered); err == nil {
		t.Fatal("expected tampered token to fail validation")
	}

	// Sanity check jwt.Parse itself would have parsed the tampered token's
	// structure fine were signature verification not enforced.
	_, _, splitErr := jwt.NewParser().ParseUnverified(tampered, jwt.MapClaims{})
	if splitErr != nil {
		t.Skip("tampering happened to break token structure, not just the signature - inconclusive")
	}
}
