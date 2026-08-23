package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

// OAuthService mirrors authentication/oauth_service.py's OAuthService: a
// generic OAuth2/OIDC authorization-code flow for user login, provider
// details (endpoints, scopes) coming entirely from the OAuthProvider model
// rather than a specific provider's SDK.
type OAuthService struct {
	db         *gorm.DB
	httpClient *http.Client
}

func NewOAuthService(db *gorm.DB) *OAuthService {
	return &OAuthService{db: db, httpClient: http.DefaultClient}
}

func (s *OAuthService) oauth2Config(provider *OAuthProvider, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		RedirectURL:  redirectURI,
		Scopes:       strings.Fields(provider.Scope),
		Endpoint: oauth2.Endpoint{
			AuthURL:  provider.AuthorizationURL,
			TokenURL: provider.TokenURL,
		},
	}
}

// GetAuthorizationURL returns the OAuth2 authorization URL and the state
// value to store for CSRF validation on callback.
func (s *OAuthService) GetAuthorizationURL(provider *OAuthProvider, redirectURI string) (authURL, state string, err error) {
	state, err = randomURLSafe(24)
	if err != nil {
		return "", "", err
	}
	cfg := s.oauth2Config(provider, redirectURI)
	return cfg.AuthCodeURL(state), state, nil
}

// ExchangeCodeForToken exchanges an authorization code for an access token.
func (s *OAuthService) ExchangeCodeForToken(ctx context.Context, provider *OAuthProvider, code, redirectURI string) (*oauth2.Token, error) {
	cfg := s.oauth2Config(provider, redirectURI)
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.httpClient)

	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("provider rejected the authorization code: %w", err)
	}
	return token, nil
}

// GetUserInfo fetches the userinfo payload from the provider's userinfo endpoint.
func (s *OAuthService) GetUserInfo(ctx context.Context, provider *OAuthProvider, accessToken string) (map[string]any, error) {
	if provider.UserinfoURL == nil || *provider.UserinfoURL == "" {
		return nil, errors.New("provider does not have userinfo_url configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *provider.UserinfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s to fetch user info: %w", provider.Name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read user info response from %s: %w", provider.Name, err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("could not fetch user info from %s (status %d)", provider.Name, resp.StatusCode)
	}

	var info map[string]any
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("%s returned an invalid user info response: %w", provider.Name, err)
	}
	return info, nil
}

// AuthenticateOrCreateUser finds or creates a User + OAuthUserToken from the
// provider's token/userinfo response, mirroring
// OAuthService.authenticate_or_create_user().
func (s *OAuthService) AuthenticateOrCreateUser(ctx context.Context, provider *OAuthProvider, token *oauth2.Token, userInfo map[string]any) (*User, *OAuthUserToken, error) {
	db := s.db.WithContext(ctx)

	providerUserID, _ := firstNonEmpty(userInfo, "sub", "id")
	email, _ := stringField(userInfo, "email")

	if providerUserID == "" {
		return nil, nil, errors.New("provider did not return user ID")
	}
	if email == "" {
		return nil, nil, errors.New("provider did not return email")
	}

	var existingToken OAuthUserToken
	err := db.Where("provider_id = ? AND provider_user_id = ?", provider.ID, providerUserID).First(&existingToken).Error

	switch {
	case err == nil:
		var user User
		if err := db.First(&user, "id = ?", existingToken.UserID).Error; err != nil {
			return nil, nil, err
		}

		existingToken.AccessToken = token.AccessToken
		if token.RefreshToken != "" {
			rt := token.RefreshToken
			existingToken.RefreshToken = &rt
		}
		if token.TokenType != "" {
			existingToken.TokenType = token.TokenType
		}
		existingToken.ProviderUserEmail = &email
		if !token.Expiry.IsZero() {
			exp := token.Expiry
			existingToken.ExpiresAt = &exp
		}
		if err := db.Save(&existingToken).Error; err != nil {
			return nil, nil, err
		}
		return &user, &existingToken, nil

	case errors.Is(err, gorm.ErrRecordNotFound):
		var user User
		userErr := db.Where("email = ?", email).First(&user).Error
		if errors.Is(userErr, gorm.ErrRecordNotFound) {
			if !provider.AutoCreateUsers {
				return nil, nil, errors.New("user does not exist and auto-creation is disabled")
			}
			username, genErr := uniqueUsernameFromEmail(db, email)
			if genErr != nil {
				return nil, nil, genErr
			}
			user = User{
				ID:       uuid.New(),
				Email:    email,
				Username: username,
				IsActive: false,
				Password: "!unusable", // mirrors Django's set_unusable_password(): no valid hash will ever match this.
			}
			if err := db.Create(&user).Error; err != nil {
				return nil, nil, err
			}
		} else if userErr != nil {
			return nil, nil, userErr
		}

		expiresAt := time.Now().UTC().Add(1 * time.Hour)
		if !token.Expiry.IsZero() {
			expiresAt = token.Expiry
		}
		var refreshToken *string
		if token.RefreshToken != "" {
			rt := token.RefreshToken
			refreshToken = &rt
		}
		tokenType := token.TokenType
		if tokenType == "" {
			tokenType = "Bearer"
		}

		newToken := OAuthUserToken{
			ID:                uuid.New(),
			UserID:            user.ID,
			ProviderID:        provider.ID,
			AccessToken:       token.AccessToken,
			RefreshToken:      refreshToken,
			TokenType:         tokenType,
			ExpiresAt:         &expiresAt,
			ProviderUserID:    providerUserID,
			ProviderUserEmail: &email,
		}
		if err := db.Create(&newToken).Error; err != nil {
			return nil, nil, err
		}
		return &user, &newToken, nil

	default:
		return nil, nil, err
	}
}

func uniqueUsernameFromEmail(db *gorm.DB, email string) (string, error) {
	base, _, _ := strings.Cut(email, "@")
	username := base
	counter := 1
	for {
		var count int64
		if err := db.Model(&User{}).Where("username = ?", username).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return username, nil
		}
		username = fmt.Sprintf("%s%d", base, counter)
		counter++
	}
}

func firstNonEmpty(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := stringField(m, k); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
