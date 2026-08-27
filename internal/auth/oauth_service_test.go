package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"controlplane/internal/auth"
)

func newTestOAuthService() (*auth.OAuthService, *fakeOAuthProviderRepository, *fakeOAuthUserTokenRepository, *fakeUserRepository) {
	providers := newFakeOAuthProviderRepository()
	tokens := newFakeOAuthUserTokenRepository()
	users := newFakeUserRepository()
	return auth.NewOAuthService(providers, tokens, users), providers, tokens, users
}

func TestExchangeCodeForToken_TranslatesProviderRejection(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()

	// Point the token URL at a server that returns an OAuth2 error response.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	}))
	defer ts.Close()

	provider := &auth.OAuthProvider{
		Name:             "Google",
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		AuthorizationURL: ts.URL + "/authorize",
		TokenURL:         ts.URL + "/token",
	}

	_, err := svc.ExchangeCodeForToken(context.Background(), provider, "bad-code", "https://app/callback")
	if err == nil {
		t.Fatal("expected an error for a rejected authorization code")
	}
}

func TestGetUserInfo_RaisesWhenNoUserinfoURL(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()

	provider := &auth.OAuthProvider{Name: "Google"}
	_, err := svc.GetUserInfo(context.Background(), provider, "access-token")
	if err == nil {
		t.Fatal("expected error when userinfo_url is not configured")
	}
}

func TestGetUserInfo_ReturnsParsedJSONOnSuccess(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-token" {
			t.Errorf("missing bearer token, got %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"sub":"123","email":"a@example.com"}`))
	}))
	defer ts.Close()

	url := ts.URL
	provider := &auth.OAuthProvider{Name: "Google", UserinfoURL: &url}
	info, err := svc.GetUserInfo(context.Background(), provider, "access-token")
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info["sub"] != "123" || info["email"] != "a@example.com" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestGetUserInfo_RaisesOnHTTPError(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	url := ts.URL
	provider := &auth.OAuthProvider{Name: "Google", UserinfoURL: &url}
	if _, err := svc.GetUserInfo(context.Background(), provider, "access-token"); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestGetUserInfo_RaisesOnUnparseableResponse(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer ts.Close()

	url := ts.URL
	provider := &auth.OAuthProvider{Name: "Google", UserinfoURL: &url}
	if _, err := svc.GetUserInfo(context.Background(), provider, "access-token"); err == nil {
		t.Fatal("expected error on unparseable response")
	}
}

func TestAuthenticateOrCreateUser_RaisesWhenProviderUserIDMissing(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()
	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: true}

	_, _, err := svc.AuthenticateOrCreateUser(context.Background(), provider, &oauth2.Token{}, map[string]any{"email": "a@example.com"})
	if err == nil {
		t.Fatal("expected error when provider user ID missing")
	}
}

func TestAuthenticateOrCreateUser_RaisesWhenEmailMissing(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()
	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: true}

	_, _, err := svc.AuthenticateOrCreateUser(context.Background(), provider, &oauth2.Token{}, map[string]any{"sub": "123"})
	if err == nil {
		t.Fatal("expected error when email missing")
	}
}

func TestAuthenticateOrCreateUser_AutoCreatesUserWhenAllowed(t *testing.T) {
	svc, _, _, users := newTestOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: true}
	token := &oauth2.Token{AccessToken: "access-token", RefreshToken: "rtok", Expiry: time.Now().Add(time.Hour)}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "new@example.com"}

	user, tok, err := svc.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.Email != "new@example.com" {
		t.Fatalf("expected new@example.com, got %s", user.Email)
	}
	if user.Password != "!unusable" {
		t.Fatalf("expected an unusable password, got %q", user.Password)
	}
	if user.IsActive {
		t.Fatal("expected new OAuth-created user to be inactive")
	}
	if tok.ProviderUserID != "provider-user-1" {
		t.Fatalf("expected provider-user-1, got %s", tok.ProviderUserID)
	}
	if tok.AccessToken != "access-token" {
		t.Fatalf("access_token = %q", tok.AccessToken)
	}

	if _, err := users.FindByID(ctx, user.ID); err != nil {
		t.Fatalf("expected user to be persisted: %v", err)
	}
}

func TestAuthenticateOrCreateUser_RejectsWhenAutoCreateDisabled(t *testing.T) {
	svc, _, _, _ := newTestOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: false}
	token := &oauth2.Token{AccessToken: "access-token"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "unknown@example.com"}

	_, _, err := svc.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err == nil {
		t.Fatal("expected an error when auto-creation is disabled and no matching user exists")
	}
}

func TestAuthenticateOrCreateUser_LinksExistingUserByEmailWhenNoOAuthTokenYet(t *testing.T) {
	svc, _, _, users := newTestOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta"}
	existing := &auth.User{ID: uuid.New(), Email: "existing@example.com", Username: "existing", IsActive: true}
	if err := users.Create(ctx, existing); err != nil {
		t.Fatalf("seed existing user: %v", err)
	}

	token := &oauth2.Token{AccessToken: "tok"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "existing@example.com"}

	user, oauthToken, err := svc.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.ID != existing.ID {
		t.Fatalf("expected existing user to be linked, got %+v", user)
	}
	if oauthToken.UserID != existing.ID {
		t.Fatalf("oauth token user_id = %v, want %v", oauthToken.UserID, existing.ID)
	}
}

func TestAuthenticateOrCreateUser_ResolvesUsernameCollisionWhenCreatingUser(t *testing.T) {
	svc, _, _, users := newTestOAuthService()
	ctx := context.Background()

	provider := &auth.OAuthProvider{ID: uuid.New(), Name: "okta", AutoCreateUsers: true}
	other := &auth.User{ID: uuid.New(), Email: "other@example.com", Username: "newperson"}
	if err := users.Create(ctx, other); err != nil {
		t.Fatalf("seed other user: %v", err)
	}

	token := &oauth2.Token{AccessToken: "tok"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "newperson@example.com"}

	user, _, err := svc.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.Username != "newperson1" {
		t.Fatalf("username = %q, want newperson1", user.Username)
	}
}

func TestAuthenticateOrCreateUser_UpdatesExistingToken(t *testing.T) {
	svc, _, tokens, users := newTestOAuthService()
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

	newToken := &oauth2.Token{AccessToken: "fresh-token", RefreshToken: "fresh-refresh", Expiry: time.Now().Add(2 * time.Hour)}
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
	if tok.RefreshToken == nil || *tok.RefreshToken != "fresh-refresh" {
		t.Fatalf("refresh_token = %v, want fresh-refresh", tok.RefreshToken)
	}
}

func TestGetActiveProviderByID_InactiveProviderReturnsNil(t *testing.T) {
	svc, providers, _, _ := newTestOAuthService()
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
