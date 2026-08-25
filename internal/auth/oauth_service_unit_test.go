package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"controlplane/internal/auth"
)

func newUnitOAuthService() (*auth.OAuthService, *fakeOAuthProviderRepository, *fakeOAuthUserTokenRepository, *fakeUserRepository) {
	providers := newFakeOAuthProviderRepository()
	tokens := newFakeOAuthUserTokenRepository()
	users := newFakeUserRepository()
	return auth.NewOAuthService(providers, tokens, users), providers, tokens, users
}

func TestAuthenticateOrCreateUser_AutoCreatesUserWhenAllowed(t *testing.T) {
	svc, _, _, users := newUnitOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: true}
	token := &oauth2.Token{AccessToken: "access-token"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "new@example.com"}

	user, tok, err := svc.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.Email != "new@example.com" {
		t.Fatalf("expected new@example.com, got %s", user.Email)
	}
	if tok.ProviderUserID != "provider-user-1" {
		t.Fatalf("expected provider-user-1, got %s", tok.ProviderUserID)
	}

	if _, err := users.FindByID(ctx, user.ID); err != nil {
		t.Fatalf("expected user to be persisted: %v", err)
	}
}

func TestAuthenticateOrCreateUser_RejectsWhenAutoCreateDisabled(t *testing.T) {
	svc, _, _, _ := newUnitOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: false}
	token := &oauth2.Token{AccessToken: "access-token"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "unknown@example.com"}

	_, _, err := svc.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err == nil {
		t.Fatal("expected an error when auto-creation is disabled and no matching user exists")
	}
}

func TestAuthenticateOrCreateUser_UpdatesExistingToken(t *testing.T) {
	svc, _, tokens, users := newUnitOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta"}
	existingUser := &auth.User{ID: uuid.New(), Email: "existing@example.com", IsActive: true}
	if err := users.Create(ctx, existingUser); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tokens.Create(ctx, &auth.OAuthUserToken{
		ID:             uuid.New(),
		UserID:         existingUser.ID,
		ProviderID:     provider.ID,
		ProviderUserID: "provider-user-1",
		AccessToken:    "stale-token",
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	newToken := &oauth2.Token{AccessToken: "fresh-token"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "existing@example.com"}

	user, tok, err := svc.AuthenticateOrCreateUser(ctx, provider, newToken, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.ID != existingUser.ID {
		t.Fatalf("expected existing user %s, got %s", existingUser.ID, user.ID)
	}
	if tok.AccessToken != "fresh-token" {
		t.Fatalf("expected token to be refreshed to fresh-token, got %s", tok.AccessToken)
	}
}

func TestGetActiveProviderByID_InactiveProviderReturnsNil(t *testing.T) {
	svc, providers, _, _ := newUnitOAuthService()
	ctx := context.Background()

	p := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", IsActive: false}
	if err := providers.Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	got, err := svc.GetActiveProviderByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetActiveProviderByID: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for inactive provider, got %+v", got)
	}
}
