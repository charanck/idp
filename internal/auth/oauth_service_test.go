package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/testutil"
)

func setupOAuth(t *testing.T) (*gorm.DB, *auth.OAuthService) {
	t.Helper()
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	return gdb, auth.NewOAuthService(gdb)
}

func makeProvider(t *testing.T, gdb *gorm.DB, mutate func(*auth.OAuthProvider)) *auth.OAuthProvider {
	t.Helper()
	userinfoURL := "https://provider.example/userinfo"
	provider := &auth.OAuthProvider{
		Name:             "Google",
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		AuthorizationURL: "https://provider.example/authorize",
		TokenURL:         "https://provider.example/token",
		UserinfoURL:      &userinfoURL,
		IsActive:         true,
		AutoCreateUsers:  true,
	}
	if mutate != nil {
		mutate(provider)
	}
	if err := gdb.Create(provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return provider
}

func TestExchangeCodeForToken_TranslatesProviderRejection(t *testing.T) {
	_, svc := setupOAuth(t)

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
	_, svc := setupOAuth(t)

	provider := &auth.OAuthProvider{Name: "Google"}
	_, err := svc.GetUserInfo(context.Background(), provider, "access-token")
	if err == nil {
		t.Fatal("expected error when userinfo_url is not configured")
	}
}

func TestGetUserInfo_ReturnsParsedJSONOnSuccess(t *testing.T) {
	_, svc := setupOAuth(t)

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
	_, svc := setupOAuth(t)

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
	_, svc := setupOAuth(t)

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
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, nil)

	_, _, err := svc.AuthenticateOrCreateUser(provider, &oauth2.Token{}, map[string]any{"email": "a@example.com"})
	if err == nil {
		t.Fatal("expected error when provider user ID missing")
	}
}

func TestAuthenticateOrCreateUser_RaisesWhenEmailMissing(t *testing.T) {
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, nil)

	_, _, err := svc.AuthenticateOrCreateUser(provider, &oauth2.Token{}, map[string]any{"sub": "123"})
	if err == nil {
		t.Fatal("expected error when email missing")
	}
}

func TestAuthenticateOrCreateUser_CreatesNewUserWhenNoneExists(t *testing.T) {
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, nil)

	token := &oauth2.Token{AccessToken: "tok", RefreshToken: "rtok", Expiry: time.Now().Add(time.Hour)}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "newperson@example.com"}

	user, oauthToken, err := svc.AuthenticateOrCreateUser(provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.Email != "newperson@example.com" {
		t.Fatalf("email = %q", user.Email)
	}
	if user.Password != "!unusable" {
		t.Fatalf("expected an unusable password, got %q", user.Password)
	}
	if user.IsActive {
		t.Fatal("expected new OAuth-created user to be inactive")
	}
	if oauthToken.ProviderUserID != "provider-user-1" {
		t.Fatalf("provider_user_id = %q", oauthToken.ProviderUserID)
	}
	if oauthToken.AccessToken != "tok" {
		t.Fatalf("access_token = %q", oauthToken.AccessToken)
	}

	var count int64
	gdb.Model(&auth.User{}).Where("email = ?", "newperson@example.com").Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 user row, got %d", count)
	}
}

func TestAuthenticateOrCreateUser_RaisesWhenAutoCreateDisabledAndUserUnknown(t *testing.T) {
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, func(p *auth.OAuthProvider) { p.AutoCreateUsers = false })

	token := &oauth2.Token{AccessToken: "tok"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "newperson@example.com"}

	_, _, err := svc.AuthenticateOrCreateUser(provider, token, userInfo)
	if err == nil {
		t.Fatal("expected error when auto-creation is disabled and user is unknown")
	}
}

func TestAuthenticateOrCreateUser_LinksExistingUserByEmailWhenNoOAuthTokenYet(t *testing.T) {
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, nil)

	existing := &auth.User{Email: "existing@example.com", Username: "existing", IsActive: true}
	if err := gdb.Create(existing).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	token := &oauth2.Token{AccessToken: "tok"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "existing@example.com"}

	user, oauthToken, err := svc.AuthenticateOrCreateUser(provider, token, userInfo)
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
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, nil)

	other := &auth.User{Email: "other@example.com", Username: "newperson"}
	if err := gdb.Create(other).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}

	token := &oauth2.Token{AccessToken: "tok"}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "newperson@example.com"}

	user, _, err := svc.AuthenticateOrCreateUser(provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if user.Username != "newperson1" {
		t.Fatalf("username = %q, want newperson1", user.Username)
	}
}

func TestAuthenticateOrCreateUser_ReusesExistingOAuthTokenAndUpdatesIt(t *testing.T) {
	gdb, svc := setupOAuth(t)
	provider := makeProvider(t, gdb, nil)

	user := &auth.User{Email: "existing@example.com", Username: "existing", IsActive: true}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	email := "existing@example.com"
	oldExpiry := time.Now().Add(time.Hour)
	existingToken := &auth.OAuthUserToken{
		UserID:            user.ID,
		ProviderID:        provider.ID,
		AccessToken:       "old-token",
		ProviderUserID:    "provider-user-1",
		ProviderUserEmail: &email,
		ExpiresAt:         &oldExpiry,
	}
	if err := gdb.Create(existingToken).Error; err != nil {
		t.Fatalf("create existing oauth token: %v", err)
	}

	token := &oauth2.Token{AccessToken: "new-token", RefreshToken: "new-refresh", Expiry: time.Now().Add(2 * time.Hour)}
	userInfo := map[string]any{"sub": "provider-user-1", "email": "existing@example.com"}

	returnedUser, oauthToken, err := svc.AuthenticateOrCreateUser(provider, token, userInfo)
	if err != nil {
		t.Fatalf("AuthenticateOrCreateUser: %v", err)
	}
	if returnedUser.ID != user.ID {
		t.Fatalf("expected same user, got %+v", returnedUser)
	}
	if oauthToken.AccessToken != "new-token" {
		t.Fatalf("access_token = %q, want new-token", oauthToken.AccessToken)
	}
	if oauthToken.RefreshToken == nil || *oauthToken.RefreshToken != "new-refresh" {
		t.Fatalf("refresh_token = %v, want new-refresh", oauthToken.RefreshToken)
	}

	var count int64
	gdb.Model(&auth.OAuthUserToken{}).Where("provider_id = ? AND provider_user_id = ?", provider.ID, "provider-user-1").Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 oauth token row, got %d", count)
	}
}
